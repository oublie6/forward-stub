#pragma once

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct skydds_writer_t skydds_writer_t;
typedef struct skydds_reader_t skydds_reader_t;

typedef struct skydds_common_options_t {
    const char* dcps_config_file;
    int domain_id;
    const char* topic_name;
    const char* message_model;
    int reliable;
    int queue_depth;
    int max_blocking_time_msec;
    const char* consumer_group;
    int compress;
} skydds_common_options_t;

int skydds_writer_open(const skydds_common_options_t* opts, skydds_writer_t** out, char* err, int err_len);
int skydds_writer_send(skydds_writer_t* writer, const uint8_t* payload, int payload_len, char* err, int err_len);
int skydds_writer_send_batch(skydds_writer_t* writer, const uint8_t** payloads, const int* payload_lens, int count, char* err, int err_len);
void skydds_writer_close(skydds_writer_t* writer);

int skydds_reader_open(const skydds_common_options_t* opts, skydds_reader_t** out, char* err, int err_len);
int skydds_reader_wait(skydds_reader_t* reader, int timeout_ms, char* err, int err_len);
int skydds_reader_drain(skydds_reader_t* reader, uint8_t* out_buf, int out_cap, int* out_lens, int lens_cap, int max_items, int* out_count, int* out_total_len, char* err, int err_len);
int skydds_reader_poll(skydds_reader_t* reader, uint8_t* out_buf, int out_cap, int timeout_ms, int* out_len, char* err, int err_len);
int skydds_reader_poll_batch(skydds_reader_t* reader, uint8_t* out_buf, int out_cap, int* out_lens, int lens_cap, int timeout_ms, int* out_count, int* out_total_len, char* err, int err_len);
void skydds_reader_close(skydds_reader_t* reader);

#ifdef __cplusplus
}
#endif
