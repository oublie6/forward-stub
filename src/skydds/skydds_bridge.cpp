//go:build skydds

#include "skydds_bridge.h"

#include <dds/DCPS/Service_Participant.h>
#include <dds/DCPS/Marked_Default_Qos.h>
#include <dds/DCPS/StaticIncludes.h>
#include <ace/OS_NS_string.h>
#include <ace/Thread_Mutex.h>
#include <ace/Condition_Thread_Mutex.h>
#include <ace/Time_Value.h>

#include <cstring>
#include <cerrno>
#include <deque>
#include <string>
#include <vector>

#include "SatelliteTypeSupportImpl.h"

namespace {

enum class Model {
    Octet,
    BatchOctet,
};

Model parse_model(const char* m) {
    std::string s = m ? m : "";
    if (s == "batch_octet") return Model::BatchOctet;
    return Model::Octet;
}

void set_error(char* err, int err_len, const std::string& msg) {
    if (!err || err_len <= 0) return;
    std::size_t n = msg.size();
    if (n >= static_cast<std::size_t>(err_len)) n = static_cast<std::size_t>(err_len - 1);
    std::memcpy(err, msg.data(), n);
    err[n] = '\0';
}

int init_factory_once(const skydds_common_options_t* opts, char* err, int err_len) {
    static bool inited = false;
    static ACE_Thread_Mutex m;
    ACE_GUARD_RETURN(ACE_Thread_Mutex, guard, m, -1);
    if (inited) return 0;
    std::string cfg = std::string("-DCPSConfigFile") + " " + (opts->dcps_config_file ? opts->dcps_config_file : "");
    char arg0[] = "forward-stub";
    std::vector<char> arg1(cfg.begin(), cfg.end());
    arg1.push_back('\0');
    char* argv[] = {arg0, arg1.data()};
    DDS::DomainParticipantFactory* dpf = TheParticipantFactoryWithArgs(2, argv);
    if (!dpf) {
        set_error(err, err_len, "TheParticipantFactoryWithArgs failed");
        return -1;
    }
    inited = true;
    return 0;
}

class OctetReaderListener : public virtual Satellite::OctetMsgDataReaderListener {
public:
    std::deque<std::vector<uint8_t>> queue_;
    ACE_Thread_Mutex lock_;
    ACE_Condition_Thread_Mutex cond_{lock_};
    bool closed_{false};

    void on_data_available(DDS::DataReader_ptr reader) override {
        Satellite::OctetMsgDataReader_var dr = Satellite::OctetMsgDataReader::_narrow(reader);
        if (!dr) return;
        Satellite::OctetMsg msg;
        DDS::SampleInfo info;
        while (dr->take_next_sample(msg, info) == DDS::RETCODE_OK) {
            if (!info.valid_data) continue;
            std::vector<uint8_t> data;
            data.reserve(static_cast<std::size_t>(msg.dataBody.length()));
            for (CORBA::ULong i = 0; i < msg.dataBody.length(); ++i) data.push_back(msg.dataBody[i]);
            ACE_GUARD(ACE_Thread_Mutex, guard, lock_);
            const bool was_empty = queue_.empty();
            queue_.push_back(std::move(data));
            if (was_empty) cond_.signal();
        }
    }

    void on_requested_deadline_missed(DDS::DataReader_ptr, const DDS::RequestedDeadlineMissedStatus&) override {}
    void on_requested_incompatible_qos(DDS::DataReader_ptr, const DDS::RequestedIncompatibleQosStatus&) override {}
    void on_liveliness_changed(DDS::DataReader_ptr, const DDS::LivelinessChangedStatus&) override {}
    void on_subscription_matched(DDS::DataReader_ptr, const DDS::SubscriptionMatchedStatus&) override {}
    void on_sample_rejected(DDS::DataReader_ptr, const DDS::SampleRejectedStatus&) override {}
    void on_sample_lost(DDS::DataReader_ptr, const DDS::SampleLostStatus&) override {}
};

class BatchReaderListener : public virtual Satellite::BatchOctetMsgDataReaderListener {
public:
    std::deque<std::vector<std::vector<uint8_t>>> queue_;
    ACE_Thread_Mutex lock_;
    ACE_Condition_Thread_Mutex cond_{lock_};
    bool closed_{false};

    void on_data_available(DDS::DataReader_ptr reader) override {
        Satellite::BatchOctetMsgDataReader_var dr = Satellite::BatchOctetMsgDataReader::_narrow(reader);
        if (!dr) return;
        Satellite::BatchOctetMsg msg;
        DDS::SampleInfo info;
        while (dr->take_next_sample(msg, info) == DDS::RETCODE_OK) {
            if (!info.valid_data) continue;
            std::vector<std::vector<uint8_t>> batch;
            batch.reserve(static_cast<std::size_t>(msg.batchData.length()));
            for (CORBA::ULong i = 0; i < msg.batchData.length(); ++i) {
                const Satellite::OctetMsg& sub = msg.batchData[i];
                std::vector<uint8_t> data;
                data.reserve(static_cast<std::size_t>(sub.dataBody.length()));
                for (CORBA::ULong j = 0; j < sub.dataBody.length(); ++j) data.push_back(sub.dataBody[j]);
                batch.push_back(std::move(data));
            }
            ACE_GUARD(ACE_Thread_Mutex, guard, lock_);
            const bool was_empty = queue_.empty();
            queue_.push_back(std::move(batch));
            if (was_empty) cond_.signal();
        }
    }

    void on_requested_deadline_missed(DDS::DataReader_ptr, const DDS::RequestedDeadlineMissedStatus&) override {}
    void on_requested_incompatible_qos(DDS::DataReader_ptr, const DDS::RequestedIncompatibleQosStatus&) override {}
    void on_liveliness_changed(DDS::DataReader_ptr, const DDS::LivelinessChangedStatus&) override {}
    void on_subscription_matched(DDS::DataReader_ptr, const DDS::SubscriptionMatchedStatus&) override {}
    void on_sample_rejected(DDS::DataReader_ptr, const DDS::SampleRejectedStatus&) override {}
    void on_sample_lost(DDS::DataReader_ptr, const DDS::SampleLostStatus&) override {}
};

} // namespace

struct skydds_writer_t {
    Model model{Model::Octet};
    DDS::DomainParticipant_var domain;
    DDS::Publisher_var publisher;
    DDS::Topic_var topic;
    DDS::DataWriter_var dw;
    Satellite::OctetMsgDataWriter_var octet_writer;
    Satellite::BatchOctetMsgDataWriter_var batch_writer;
};

struct skydds_reader_t {
    Model model{Model::Octet};
    DDS::DomainParticipant_var domain;
    DDS::Subscriber_var sub;
    DDS::Topic_var topic;
    DDS::DataReader_var dr;
    Satellite::OctetMsgDataReader_var octet_reader;
    Satellite::BatchOctetMsgDataReader_var batch_reader;
    OctetReaderListener* octet_listener{};
    BatchReaderListener* batch_listener{};
};

int skydds_writer_open(const skydds_common_options_t* opts, skydds_writer_t** out, char* err, int err_len) {
    if (!opts || !out) return -1;
    if (init_factory_once(opts, err, err_len) != 0) return -1;
    auto* h = new skydds_writer_t();
    h->model = parse_model(opts->message_model);

    h->domain = TheParticipantFactory->create_participant(opts->domain_id, PARTICIPANT_QOS_DEFAULT, 0, SkyDDS::DCPS::DEFAULT_STATUS_MASK);
    if (!h->domain) { set_error(err, err_len, "create_participant failed"); delete h; return -1; }

    const char* type_name = h->model == Model::BatchOctet ? "Satellite::BatchOctetMsg" : "Satellite::OctetMsg";
    if (h->model == Model::BatchOctet) {
        Satellite::BatchOctetMsgTypeSupport_var ts = new Satellite::BatchOctetMsgTypeSupportImpl;
        if (ts->register_type(h->domain, type_name) != DDS::RETCODE_OK) {
            set_error(err, err_len, "register_type Satellite::BatchOctetMsg failed"); delete h; return -1;
        }
    } else {
        Satellite::OctetMsgTypeSupport_var ts = new Satellite::OctetMsgTypeSupportImpl;
        if (ts->register_type(h->domain, type_name) != DDS::RETCODE_OK) {
            set_error(err, err_len, "register_type Satellite::OctetMsg failed"); delete h; return -1;
        }
    }

    h->topic = h->domain->create_topic(opts->topic_name, type_name, TOPIC_QOS_DEFAULT, 0, SkyDDS::DCPS::DEFAULT_STATUS_MASK);
    if (!h->topic) { set_error(err, err_len, "create_topic failed"); delete h; return -1; }

    h->publisher = h->domain->create_publisher(PUBLISHER_QOS_DEFAULT, 0, SkyDDS::DCPS::DEFAULT_STATUS_MASK);
    if (!h->publisher) { set_error(err, err_len, "create_publisher failed"); delete h; return -1; }

    h->dw = h->publisher->create_datawriter(h->topic, DATAWRITER_QOS_DEFAULT, 0, SkyDDS::DCPS::DEFAULT_STATUS_MASK);
    if (!h->dw) { set_error(err, err_len, "create_datawriter failed"); delete h; return -1; }

    if (h->model == Model::BatchOctet) {
        h->batch_writer = Satellite::BatchOctetMsgDataWriter::_narrow(h->dw);
        if (!h->batch_writer) { set_error(err, err_len, "_narrow BatchOctetMsgDataWriter failed"); delete h; return -1; }
    } else {
        h->octet_writer = Satellite::OctetMsgDataWriter::_narrow(h->dw);
        if (!h->octet_writer) { set_error(err, err_len, "_narrow OctetMsgDataWriter failed"); delete h; return -1; }
    }
    *out = h;
    return 0;
}

int skydds_writer_send(skydds_writer_t* writer, const uint8_t* payload, int payload_len, char* err, int err_len) {
    if (!writer) return -1;
    if (writer->model != Model::Octet || !writer->octet_writer) {
        set_error(err, err_len, "writer message_model is not octet");
        return -1;
    }
    if (payload_len < 0) return -1;

    Satellite::OctetMsg msg;
    msg.dataLen = payload_len;
    msg.dataBody.length(static_cast<CORBA::ULong>(payload_len));
    for (int i = 0; i < payload_len; ++i) msg.dataBody[static_cast<CORBA::ULong>(i)] = payload[i];

    DDS::ReturnCode_t rc = writer->octet_writer->write(msg, DDS::HANDLE_NIL);
    if (rc != DDS::RETCODE_OK) {
        set_error(err, err_len, "OctetMsgDataWriter::write failed");
        return static_cast<int>(rc);
    }
    return 0;
}

int skydds_writer_send_batch(skydds_writer_t* writer, const uint8_t** payloads, const int* payload_lens, int count, char* err, int err_len) {
    if (!writer) return -1;
    if (writer->model != Model::BatchOctet || !writer->batch_writer) {
        set_error(err, err_len, "writer message_model is not batch_octet");
        return -1;
    }
    if (count <= 0) return 0;

    Satellite::BatchOctetMsg batch;
    batch.batchData.length(static_cast<CORBA::ULong>(count));
    for (int i = 0; i < count; ++i) {
        int len = payload_lens[i];
        if (len < 0) len = 0;
        Satellite::OctetMsg& sub = batch.batchData[static_cast<CORBA::ULong>(i)];
        sub.dataLen = len;
        sub.dataBody.length(static_cast<CORBA::ULong>(len));
        for (int j = 0; j < len; ++j) sub.dataBody[static_cast<CORBA::ULong>(j)] = payloads[i][j];
    }

    DDS::ReturnCode_t rc = writer->batch_writer->write(batch, DDS::HANDLE_NIL);
    if (rc != DDS::RETCODE_OK) {
        set_error(err, err_len, "BatchOctetMsgDataWriter::write failed");
        return static_cast<int>(rc);
    }
    return 0;
}

void skydds_writer_close(skydds_writer_t* writer) { delete writer; }

int skydds_reader_open(const skydds_common_options_t* opts, skydds_reader_t** out, char* err, int err_len) {
    if (!opts || !out) return -1;
    if (init_factory_once(opts, err, err_len) != 0) return -1;
    auto* h = new skydds_reader_t();
    h->model = parse_model(opts->message_model);

    h->domain = TheParticipantFactory->create_participant(opts->domain_id, PARTICIPANT_QOS_DEFAULT, 0, SkyDDS::DCPS::DEFAULT_STATUS_MASK);
    if (!h->domain) { set_error(err, err_len, "create_participant failed"); delete h; return -1; }

    const char* type_name = h->model == Model::BatchOctet ? "Satellite::BatchOctetMsg" : "Satellite::OctetMsg";
    if (h->model == Model::BatchOctet) {
        Satellite::BatchOctetMsgTypeSupport_var ts = new Satellite::BatchOctetMsgTypeSupportImpl;
        if (ts->register_type(h->domain, type_name) != DDS::RETCODE_OK) {
            set_error(err, err_len, "register_type Satellite::BatchOctetMsg failed"); delete h; return -1;
        }
    } else {
        Satellite::OctetMsgTypeSupport_var ts = new Satellite::OctetMsgTypeSupportImpl;
        if (ts->register_type(h->domain, type_name) != DDS::RETCODE_OK) {
            set_error(err, err_len, "register_type Satellite::OctetMsg failed"); delete h; return -1;
        }
    }

    h->topic = h->domain->create_topic(opts->topic_name, type_name, TOPIC_QOS_DEFAULT, 0, SkyDDS::DCPS::DEFAULT_STATUS_MASK);
    if (!h->topic) { set_error(err, err_len, "create_topic failed"); delete h; return -1; }

    h->sub = h->domain->create_subscriber(SUBSCRIBER_QOS_DEFAULT, 0, SkyDDS::DCPS::DEFAULT_STATUS_MASK);
    if (!h->sub) { set_error(err, err_len, "create_subscriber failed"); delete h; return -1; }

    if (h->model == Model::BatchOctet) {
        h->batch_listener = new BatchReaderListener();
        h->dr = h->sub->create_datareader(h->topic, DATAREADER_QOS_DEFAULT, h->batch_listener, SkyDDS::DCPS::DEFAULT_STATUS_MASK);
        if (!h->dr) { set_error(err, err_len, "create_datareader failed"); delete h->batch_listener; delete h; return -1; }
        h->batch_reader = Satellite::BatchOctetMsgDataReader::_narrow(h->dr);
        if (!h->batch_reader) { set_error(err, err_len, "_narrow BatchOctetMsgDataReader failed"); delete h->batch_listener; delete h; return -1; }
    } else {
        h->octet_listener = new OctetReaderListener();
        h->dr = h->sub->create_datareader(h->topic, DATAREADER_QOS_DEFAULT, h->octet_listener, SkyDDS::DCPS::DEFAULT_STATUS_MASK);
        if (!h->dr) { set_error(err, err_len, "create_datareader failed"); delete h->octet_listener; delete h; return -1; }
        h->octet_reader = Satellite::OctetMsgDataReader::_narrow(h->dr);
        if (!h->octet_reader) { set_error(err, err_len, "_narrow OctetMsgDataReader failed"); delete h->octet_listener; delete h; return -1; }
    }

    *out = h;
    return 0;
}

int skydds_reader_poll(skydds_reader_t* reader, uint8_t* out_buf, int out_cap, int timeout_ms, int* out_len, char* err, int err_len) {
    (void)timeout_ms;
    if (!reader || !out_len) return -1;
    if (reader->model != Model::Octet || !reader->octet_listener) {
        set_error(err, err_len, "reader message_model is not octet");
        return -1;
    }
    *out_len = 0;

    ACE_GUARD_RETURN(ACE_Thread_Mutex, guard, reader->octet_listener->lock_, -1);
    if (reader->octet_listener->queue_.empty()) {
        return 1;
    }
    std::vector<uint8_t> msg = std::move(reader->octet_listener->queue_.front());
    reader->octet_listener->queue_.pop_front();
    if (static_cast<int>(msg.size()) > out_cap) {
        set_error(err, err_len, "receiver output buffer too small");
        return -2;
    }
    std::memcpy(out_buf, msg.data(), msg.size());
    *out_len = static_cast<int>(msg.size());
    return 0;
}

int skydds_reader_poll_batch(skydds_reader_t* reader, uint8_t* out_buf, int out_cap, int* out_lens, int lens_cap, int timeout_ms, int* out_count, int* out_total_len, char* err, int err_len) {
    (void)timeout_ms;
    if (!reader || !out_count || !out_total_len) return -1;
    if (reader->model != Model::BatchOctet || !reader->batch_listener) {
        set_error(err, err_len, "reader message_model is not batch_octet");
        return -1;
    }

    *out_count = 0;
    *out_total_len = 0;

    ACE_GUARD_RETURN(ACE_Thread_Mutex, guard, reader->batch_listener->lock_, -1);
    if (reader->batch_listener->queue_.empty()) {
        return 1;
    }

    std::vector<std::vector<uint8_t>> batch = std::move(reader->batch_listener->queue_.front());
    reader->batch_listener->queue_.pop_front();
    if (static_cast<int>(batch.size()) > lens_cap) {
        set_error(err, err_len, "receiver lens buffer too small");
        return -2;
    }

    int offset = 0;
    for (int i = 0; i < static_cast<int>(batch.size()); ++i) {
        int len = static_cast<int>(batch[i].size());
        if (offset + len > out_cap) {
            set_error(err, err_len, "receiver output buffer too small for batch payload");
            return -3;
        }
        if (len > 0) {
            std::memcpy(out_buf + offset, batch[i].data(), len);
        }
        out_lens[i] = len;
        offset += len;
    }
    *out_count = static_cast<int>(batch.size());
    *out_total_len = offset;
    return 0;
}

int skydds_reader_wait(skydds_reader_t* reader, int timeout_ms, char* err, int err_len) {
    if (!reader) return -1;
    if (timeout_ms < 0) timeout_ms = 0;

    if (reader->model == Model::BatchOctet) {
        if (!reader->batch_listener) {
            set_error(err, err_len, "batch listener is nil");
            return -1;
        }
        ACE_GUARD_RETURN(ACE_Thread_Mutex, guard, reader->batch_listener->lock_, -1);
        if (!reader->batch_listener->queue_.empty()) return 0;
        if (reader->batch_listener->closed_) return -2;
        ACE_Time_Value timeout = ACE_OS::gettimeofday() + ACE_Time_Value(0, static_cast<long>(timeout_ms) * 1000);
        const int rc = reader->batch_listener->cond_.wait(&timeout);
        if (rc == -1 && errno == ETIME) return 1;
        if (reader->batch_listener->closed_) return -2;
        return reader->batch_listener->queue_.empty() ? 1 : 0;
    }

    if (!reader->octet_listener) {
        set_error(err, err_len, "octet listener is nil");
        return -1;
    }
    ACE_GUARD_RETURN(ACE_Thread_Mutex, guard, reader->octet_listener->lock_, -1);
    if (!reader->octet_listener->queue_.empty()) return 0;
    if (reader->octet_listener->closed_) return -2;
    ACE_Time_Value timeout = ACE_OS::gettimeofday() + ACE_Time_Value(0, static_cast<long>(timeout_ms) * 1000);
    const int rc = reader->octet_listener->cond_.wait(&timeout);
    if (rc == -1 && errno == ETIME) return 1;
    if (reader->octet_listener->closed_) return -2;
    return reader->octet_listener->queue_.empty() ? 1 : 0;
}

int skydds_reader_drain(skydds_reader_t* reader, uint8_t* out_buf, int out_cap, int* out_lens, int lens_cap, int max_items, int* out_count, int* out_total_len, char* err, int err_len) {
    if (!reader || !out_count || !out_total_len) return -1;
    if (max_items <= 0) max_items = lens_cap;
    *out_count = 0;
    *out_total_len = 0;

    if (reader->model == Model::BatchOctet) {
        if (!reader->batch_listener) {
            set_error(err, err_len, "batch listener is nil");
            return -1;
        }
        ACE_GUARD_RETURN(ACE_Thread_Mutex, guard, reader->batch_listener->lock_, -1);
        int offset = 0;
        while (!reader->batch_listener->queue_.empty() && *out_count < max_items && *out_count < lens_cap) {
            std::vector<std::vector<uint8_t>>& batch = reader->batch_listener->queue_.front();
            while (!batch.empty() && *out_count < max_items && *out_count < lens_cap) {
                std::vector<uint8_t>& sub = batch.front();
                const int len = static_cast<int>(sub.size());
                if (offset + len > out_cap) {
                    if (*out_count > 0) {
                        *out_total_len = offset;
                        return 0;
                    }
                    set_error(err, err_len, "receiver output buffer too small");
                    return -3;
                }
                if (len > 0) std::memcpy(out_buf + offset, sub.data(), static_cast<std::size_t>(len));
                out_lens[*out_count] = len;
                offset += len;
                ++(*out_count);
                batch.erase(batch.begin());
            }
            if (batch.empty()) reader->batch_listener->queue_.pop_front();
            if (*out_count >= max_items || *out_count >= lens_cap) break;
        }
        *out_total_len = offset;
        return 0;
    }

    if (!reader->octet_listener) {
        set_error(err, err_len, "octet listener is nil");
        return -1;
    }
    ACE_GUARD_RETURN(ACE_Thread_Mutex, guard, reader->octet_listener->lock_, -1);
    int offset = 0;
    while (!reader->octet_listener->queue_.empty() && *out_count < max_items && *out_count < lens_cap) {
        std::vector<uint8_t> msg = std::move(reader->octet_listener->queue_.front());
        reader->octet_listener->queue_.pop_front();
        const int len = static_cast<int>(msg.size());
        if (offset + len > out_cap) {
            if (*out_count > 0) {
                *out_total_len = offset;
                return 0;
            }
            set_error(err, err_len, "receiver output buffer too small");
            return -2;
        }
        if (len > 0) std::memcpy(out_buf + offset, msg.data(), static_cast<std::size_t>(len));
        out_lens[*out_count] = len;
        offset += len;
        ++(*out_count);
    }
    *out_total_len = offset;
    return 0;
}

void skydds_reader_close(skydds_reader_t* reader) {
    if (!reader) return;
    if (reader->octet_listener) {
        ACE_GUARD(ACE_Thread_Mutex, guard, reader->octet_listener->lock_);
        reader->octet_listener->closed_ = true;
        reader->octet_listener->cond_.broadcast();
    }
    if (reader->batch_listener) {
        ACE_GUARD(ACE_Thread_Mutex, guard, reader->batch_listener->lock_);
        reader->batch_listener->closed_ = true;
        reader->batch_listener->cond_.broadcast();
    }
    delete reader->octet_listener;
    delete reader->batch_listener;
    delete reader;
}
