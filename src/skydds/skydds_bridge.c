//go:build skydds

#include "skydds_bridge.h"

#include <pthread.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "SatelliteDDSWrapper.h"

enum {
    skydds_queue_soft_limit = 65536,
    skydds_pump_idle_sleep_ns = 1000000,
    skydds_pump_error_sleep_ns = 10000000,
    skydds_err_queue_full = -4,
};

typedef struct skydds_msg_node_t {
    struct skydds_msg_node_t* next;
    int len;
    uint8_t data[];
} skydds_msg_node_t;

struct skydds_writer_t {
    char* topic;
    int compress;
};

struct skydds_reader_t {
    void* dr;
    char* topic;
    int compress;
    pthread_t pump;
    pthread_mutex_t mu;
    pthread_cond_t cond;
    skydds_msg_node_t* head;
    skydds_msg_node_t* tail;
    int queued;
    int closed;
    int pump_started;
    int pump_error;
};

static pthread_mutex_t g_init_mu = PTHREAD_MUTEX_INITIALIZER;
static int g_init_refs = 0;
static int g_init_domain_id = -1;
static char* g_init_cfg = NULL;

static void set_error(char* err, int err_len, const char* msg) {
    if (!err || err_len <= 0) return;
    if (!msg) msg = "";
    size_t n = strlen(msg);
    if (n >= (size_t)err_len) n = (size_t)err_len - 1;
    memcpy(err, msg, n);
    err[n] = '\0';
}

static char* dup_cstr(const char* s) {
    if (!s) s = "";
    size_t n = strlen(s);
    char* out = (char*)malloc(n + 1);
    if (!out) return NULL;
    memcpy(out, s, n + 1);
    return out;
}

static int same_cstr(const char* a, const char* b) {
    if (!a) a = "";
    if (!b) b = "";
    return strcmp(a, b) == 0;
}

static DDSQos build_qos(const skydds_common_options_t* opts) {
    DDSQos qos;
    qos.reliable = opts ? opts->reliable : 0;
    qos.depth = opts ? opts->queue_depth : 0;
    qos.max_blocking_time_msec = opts ? opts->max_blocking_time_msec : 0;
    return qos;
}

static int retain_dds(const skydds_common_options_t* opts, char* err, int err_len) {
    if (!opts) {
        set_error(err, err_len, "nil options");
        return -1;
    }
    pthread_mutex_lock(&g_init_mu);
    if (g_init_refs > 0) {
        if (g_init_domain_id != opts->domain_id || !same_cstr(g_init_cfg, opts->dcps_config_file)) {
            pthread_mutex_unlock(&g_init_mu);
            set_error(err, err_len, "SkyDDS already initialized with different dcps_config_file or domain_id");
            return -1;
        }
        g_init_refs++;
        pthread_mutex_unlock(&g_init_mu);
        return 0;
    }

    int rc = ddsInit(opts->dcps_config_file, opts->domain_id, 0);
    if (rc != 0) {
        pthread_mutex_unlock(&g_init_mu);
        if (rc == -1) {
            set_error(err, err_len, "ddsInit failed: SKY_DDS is not defined");
        } else if (rc == -2) {
            set_error(err, err_len, "ddsInit failed: system initialization failed, check $SKY_DDS/log/skydds.log");
        } else {
            set_error(err, err_len, "ddsInit failed");
        }
        return rc;
    }

    g_init_cfg = dup_cstr(opts->dcps_config_file);
    if (!g_init_cfg) {
        ddsUnInit();
        pthread_mutex_unlock(&g_init_mu);
        set_error(err, err_len, "out of memory");
        return -1;
    }
    g_init_domain_id = opts->domain_id;
    g_init_refs = 1;
    pthread_mutex_unlock(&g_init_mu);
    return 0;
}

static void release_dds(void) {
    pthread_mutex_lock(&g_init_mu);
    if (g_init_refs > 0) {
        g_init_refs--;
        if (g_init_refs == 0) {
            ddsUnInit();
            free(g_init_cfg);
            g_init_cfg = NULL;
            g_init_domain_id = -1;
        }
    }
    pthread_mutex_unlock(&g_init_mu);
}

static void free_queue(skydds_reader_t* r) {
    skydds_msg_node_t* n = r->head;
    while (n) {
        skydds_msg_node_t* next = n->next;
        free(n);
        n = next;
    }
    r->head = NULL;
    r->tail = NULL;
    r->queued = 0;
}

static int enqueue_msg(skydds_reader_t* r, const void* data, int len) {
    if (len < 0) len = 0;
    skydds_msg_node_t* n = (skydds_msg_node_t*)malloc(sizeof(*n) + (size_t)len);
    if (!n) return -1;
    n->next = NULL;
    n->len = len;
    if (len > 0 && data) memcpy(n->data, data, (size_t)len);

    pthread_mutex_lock(&r->mu);
    if (r->closed || r->queued >= skydds_queue_soft_limit) {
        pthread_mutex_unlock(&r->mu);
        free(n);
        return r->closed ? 0 : skydds_err_queue_full;
    }
    int was_empty = (r->queued == 0);
    if (r->tail) {
        r->tail->next = n;
    } else {
        r->head = n;
    }
    r->tail = n;
    r->queued++;
    if (was_empty) pthread_cond_signal(&r->cond);
    pthread_mutex_unlock(&r->mu);
    return 0;
}

static int reader_closed(skydds_reader_t* r) {
    pthread_mutex_lock(&r->mu);
    int closed = r->closed;
    pthread_mutex_unlock(&r->mu);
    return closed;
}

static void sleep_ns(long ns) {
    struct timespec ts;
    ts.tv_sec = ns / 1000000000L;
    ts.tv_nsec = ns % 1000000000L;
    nanosleep(&ts, NULL);
}

static void* reader_pump(void* arg) {
    skydds_reader_t* r = (skydds_reader_t*)arg;
    while (!reader_closed(r)) {
        DDSSamplesList list;
        memset(&list, 0, sizeof(list));
        takeDDSMessages(r->dr, &list, r->compress);
        if (list.num <= 0) {
            freeDDSMessages(&list);
            sleep_ns(skydds_pump_idle_sleep_ns);
            continue;
        }

        for (int i = 0; i < list.num; i++) {
            DDSSample* sample = list.samples + i;
            int rc = enqueue_msg(r, sample->data, sample->len);
            if (rc < 0) {
                pthread_mutex_lock(&r->mu);
                r->pump_error = rc;
                pthread_cond_signal(&r->cond);
                pthread_mutex_unlock(&r->mu);
                sleep_ns(skydds_pump_error_sleep_ns);
                if (reader_closed(r)) break;
            }
        }
        freeDDSMessages(&list);
    }

    pthread_mutex_lock(&r->mu);
    r->closed = 1;
    pthread_cond_broadcast(&r->cond);
    pthread_mutex_unlock(&r->mu);
    return NULL;
}

int skydds_writer_open(const skydds_common_options_t* opts, skydds_writer_t** out, char* err, int err_len) {
    if (!opts || !out) return -1;
    *out = NULL;
    if (!opts->topic_name || opts->topic_name[0] == '\0') {
        set_error(err, err_len, "topic_name is required");
        return -1;
    }
    if (retain_dds(opts, err, err_len) != 0) return -1;

    skydds_writer_t* w = (skydds_writer_t*)calloc(1, sizeof(*w));
    if (!w) {
        release_dds();
        set_error(err, err_len, "out of memory");
        return -1;
    }
    w->topic = dup_cstr(opts->topic_name);
    w->compress = opts->compress;
    if (!w->topic) {
        free(w);
        release_dds();
        set_error(err, err_len, "out of memory");
        return -1;
    }

    DDSQos qos = build_qos(opts);
    void* dw = ddsCreateDataWriter(w->topic, &qos, NULL);
    if (!dw) {
        free(w->topic);
        free(w);
        release_dds();
        set_error(err, err_len, "ddsCreateDataWriter failed");
        return -1;
    }

    *out = w;
    return 0;
}

int skydds_writer_send(skydds_writer_t* writer, const uint8_t* payload, int payload_len, char* err, int err_len) {
    if (!writer) return -1;
    if (payload_len < 0) {
        set_error(err, err_len, "payload_len must be >= 0");
        return -1;
    }
    int rc = ddsWriteMessage(writer->topic, payload_len, payload, writer->compress);
    if (rc == 10) {
        set_error(err, err_len, "ddsWriteMessage failed: SkyDDS queue full");
        return 10;
    }
    if (rc != 0) {
        set_error(err, err_len, "ddsWriteMessage failed");
        return rc;
    }
    return 0;
}

int skydds_writer_send_batch(skydds_writer_t* writer, const uint8_t** payloads, const int* payload_lens, int count, char* err, int err_len) {
    if (!writer) return -1;
    if (count <= 0) return 0;
    if (!payloads || !payload_lens) {
        set_error(err, err_len, "nil batch payloads");
        return -1;
    }
    for (int i = 0; i < count; i++) {
        int rc = skydds_writer_send(writer, payloads[i], payload_lens[i], err, err_len);
        if (rc != 0) return rc;
    }
    return 0;
}

void skydds_writer_close(skydds_writer_t* writer) {
    if (!writer) return;
    free(writer->topic);
    free(writer);
    release_dds();
}

int skydds_reader_open(const skydds_common_options_t* opts, skydds_reader_t** out, char* err, int err_len) {
    if (!opts || !out) return -1;
    *out = NULL;
    if (!opts->topic_name || opts->topic_name[0] == '\0') {
        set_error(err, err_len, "topic_name is required");
        return -1;
    }
    if (retain_dds(opts, err, err_len) != 0) return -1;

    skydds_reader_t* r = (skydds_reader_t*)calloc(1, sizeof(*r));
    if (!r) {
        release_dds();
        set_error(err, err_len, "out of memory");
        return -1;
    }
    r->topic = dup_cstr(opts->topic_name);
    r->compress = opts->compress;
    pthread_mutex_init(&r->mu, NULL);
    pthread_cond_init(&r->cond, NULL);
    if (!r->topic) {
        pthread_cond_destroy(&r->cond);
        pthread_mutex_destroy(&r->mu);
        free(r);
        release_dds();
        set_error(err, err_len, "out of memory");
        return -1;
    }

    DDSQos qos = build_qos(opts);
    if (opts->consumer_group && opts->consumer_group[0] != '\0') {
        r->dr = ddsCreateDataReaderOnGroup(r->topic, opts->consumer_group, &qos, NULL, r->compress);
    } else {
        r->dr = ddsCreateDataReader(r->topic, &qos, NULL, r->compress);
    }
    if (!r->dr) {
        free(r->topic);
        pthread_cond_destroy(&r->cond);
        pthread_mutex_destroy(&r->mu);
        free(r);
        release_dds();
        set_error(err, err_len, "ddsCreateDataReader failed");
        return -1;
    }

    if (pthread_create(&r->pump, NULL, reader_pump, r) != 0) {
        free(r->topic);
        pthread_cond_destroy(&r->cond);
        pthread_mutex_destroy(&r->mu);
        free(r);
        release_dds();
        set_error(err, err_len, "create reader pump thread failed");
        return -1;
    }
    r->pump_started = 1;
    *out = r;
    return 0;
}

int skydds_reader_wait(skydds_reader_t* reader, int timeout_ms, char* err, int err_len) {
    if (!reader) return -1;
    if (timeout_ms < 0) timeout_ms = 0;

    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    ts.tv_sec += timeout_ms / 1000;
    ts.tv_nsec += (long)(timeout_ms % 1000) * 1000000L;
    if (ts.tv_nsec >= 1000000000L) {
        ts.tv_sec++;
        ts.tv_nsec -= 1000000000L;
    }

    pthread_mutex_lock(&reader->mu);
    while (!reader->closed && reader->queued == 0 && reader->pump_error == 0) {
        int rc = pthread_cond_timedwait(&reader->cond, &reader->mu, &ts);
        if (rc != 0) break;
    }
    int out = 1;
    if (reader->queued > 0) {
        out = 0;
    } else if (reader->closed) {
        out = -2;
    } else if (reader->pump_error != 0) {
        set_error(err, err_len, "SkyDDS reader pump queue overflow");
        out = reader->pump_error;
        reader->pump_error = 0;
    }
    pthread_mutex_unlock(&reader->mu);
    return out;
}

int skydds_reader_drain(skydds_reader_t* reader, uint8_t* out_buf, int out_cap, int* out_lens, int lens_cap, int max_items, int* out_count, int* out_total_len, char* err, int err_len) {
    if (!reader || !out_count || !out_total_len) return -1;
    *out_count = 0;
    *out_total_len = 0;
    if (max_items <= 0) max_items = lens_cap;
    if (!out_buf || out_cap < 0 || !out_lens || lens_cap <= 0) {
        set_error(err, err_len, "invalid drain buffer");
        return -1;
    }

    pthread_mutex_lock(&reader->mu);
    int offset = 0;
    while (reader->head && *out_count < max_items && *out_count < lens_cap) {
        skydds_msg_node_t* n = reader->head;
        if (offset + n->len > out_cap) {
            if (*out_count > 0) break;
            pthread_mutex_unlock(&reader->mu);
            set_error(err, err_len, "receiver output buffer too small");
            return -2;
        }
        reader->head = n->next;
        if (!reader->head) reader->tail = NULL;
        reader->queued--;
        if (n->len > 0) memcpy(out_buf + offset, n->data, (size_t)n->len);
        out_lens[*out_count] = n->len;
        offset += n->len;
        (*out_count)++;
        free(n);
    }
    *out_total_len = offset;
    pthread_mutex_unlock(&reader->mu);
    return 0;
}

int skydds_reader_poll(skydds_reader_t* reader, uint8_t* out_buf, int out_cap, int timeout_ms, int* out_len, char* err, int err_len) {
    if (!out_len) return -1;
    *out_len = 0;
    int ready = skydds_reader_wait(reader, timeout_ms, err, err_len);
    if (ready != 0) return ready;
    int count = 0;
    int total = 0;
    int len = 0;
    int rc = skydds_reader_drain(reader, out_buf, out_cap, &len, 1, 1, &count, &total, err, err_len);
    if (rc == 0 && count == 1) *out_len = len;
    return rc;
}

int skydds_reader_poll_batch(skydds_reader_t* reader, uint8_t* out_buf, int out_cap, int* out_lens, int lens_cap, int timeout_ms, int* out_count, int* out_total_len, char* err, int err_len) {
    int ready = skydds_reader_wait(reader, timeout_ms, err, err_len);
    if (ready != 0) {
        if (out_count) *out_count = 0;
        if (out_total_len) *out_total_len = 0;
        return ready;
    }
    return skydds_reader_drain(reader, out_buf, out_cap, out_lens, lens_cap, lens_cap, out_count, out_total_len, err, err_len);
}

void skydds_reader_close(skydds_reader_t* reader) {
    if (!reader) return;
    pthread_mutex_lock(&reader->mu);
    reader->closed = 1;
    pthread_cond_broadcast(&reader->cond);
    pthread_mutex_unlock(&reader->mu);

    if (reader->pump_started) pthread_join(reader->pump, NULL);

    pthread_mutex_lock(&reader->mu);
    free_queue(reader);
    pthread_mutex_unlock(&reader->mu);
    free(reader->topic);
    pthread_cond_destroy(&reader->cond);
    pthread_mutex_destroy(&reader->mu);
    free(reader);
    release_dds();
}
