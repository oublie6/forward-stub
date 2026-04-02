//go:build skydds

#include "skydds_bridge.h"

#include <dds/DCPS/Service_Participant.h>
#include <dds/DCPS/Marked_Default_Qos.h>
#include <dds/DCPS/StaticIncludes.h>
#include <ace/OS_NS_string.h>
#include <ace/Thread_Mutex.h>

#include <deque>
#include <string>
#include <vector>

#include "SatelliteTypeSupportImpl.h"

namespace {

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
            queue_.push_back(std::move(data));
        }
    }

    // unused callbacks
    void on_requested_deadline_missed(DDS::DataReader_ptr, const DDS::RequestedDeadlineMissedStatus&) override {}
    void on_requested_incompatible_qos(DDS::DataReader_ptr, const DDS::RequestedIncompatibleQosStatus&) override {}
    void on_liveliness_changed(DDS::DataReader_ptr, const DDS::LivelinessChangedStatus&) override {}
    void on_subscription_matched(DDS::DataReader_ptr, const DDS::SubscriptionMatchedStatus&) override {}
    void on_sample_rejected(DDS::DataReader_ptr, const DDS::SampleRejectedStatus&) override {}
    void on_sample_lost(DDS::DataReader_ptr, const DDS::SampleLostStatus&) override {}
};

} // namespace

struct skydds_writer_t {
    DDS::DomainParticipant_var domain;
    DDS::Publisher_var publisher;
    DDS::Topic_var topic;
    DDS::DataWriter_var dw;
    Satellite::OctetMsgDataWriter_var writer;
};

struct skydds_reader_t {
    DDS::DomainParticipant_var domain;
    DDS::Subscriber_var sub;
    DDS::Topic_var topic;
    DDS::DataReader_var dr;
    Satellite::OctetMsgDataReader_var reader;
    OctetReaderListener* listener{};
};

int skydds_writer_open(const skydds_common_options_t* opts, skydds_writer_t** out, char* err, int err_len) {
    if (!opts || !out) return -1;
    if (init_factory_once(opts, err, err_len) != 0) return -1;
    auto* h = new skydds_writer_t();

    h->domain = TheParticipantFactory->create_participant(opts->domain_id, PARTICIPANT_QOS_DEFAULT, 0, SkyDDS::DCPS::DEFAULT_STATUS_MASK);
    if (!h->domain) { set_error(err, err_len, "create_participant failed"); delete h; return -1; }

    Satellite::OctetMsgTypeSupport_var ts = new Satellite::OctetMsgTypeSupportImpl;
    if (ts->register_type(h->domain, "Satellite::OctetMsg") != DDS::RETCODE_OK) {
        set_error(err, err_len, "register_type Satellite::OctetMsg failed"); delete h; return -1;
    }

    h->topic = h->domain->create_topic(opts->topic_name, "Satellite::OctetMsg", TOPIC_QOS_DEFAULT, 0, SkyDDS::DCPS::DEFAULT_STATUS_MASK);
    if (!h->topic) { set_error(err, err_len, "create_topic failed"); delete h; return -1; }

    h->publisher = h->domain->create_publisher(PUBLISHER_QOS_DEFAULT, 0, SkyDDS::DCPS::DEFAULT_STATUS_MASK);
    if (!h->publisher) { set_error(err, err_len, "create_publisher failed"); delete h; return -1; }

    h->dw = h->publisher->create_datawriter(h->topic, DATAWRITER_QOS_DEFAULT, 0, SkyDDS::DCPS::DEFAULT_STATUS_MASK);
    if (!h->dw) { set_error(err, err_len, "create_datawriter failed"); delete h; return -1; }

    h->writer = Satellite::OctetMsgDataWriter::_narrow(h->dw);
    if (!h->writer) { set_error(err, err_len, "_narrow OctetMsgDataWriter failed"); delete h; return -1; }
    *out = h;
    return 0;
}

int skydds_writer_send(skydds_writer_t* writer, const uint8_t* payload, int payload_len, char* err, int err_len) {
    if (!writer || !writer->writer) return -1;
    if (payload_len < 0) return -1;

    Satellite::OctetMsg msg;
    msg.dataLen = payload_len;
    msg.dataBody.length(static_cast<CORBA::ULong>(payload_len));
    for (int i = 0; i < payload_len; ++i) msg.dataBody[static_cast<CORBA::ULong>(i)] = payload[i];

    DDS::ReturnCode_t rc = writer->writer->write(msg, DDS::HANDLE_NIL);
    if (rc != DDS::RETCODE_OK) {
        set_error(err, err_len, "OctetMsgDataWriter::write failed");
        return static_cast<int>(rc);
    }
    return 0;
}

void skydds_writer_close(skydds_writer_t* writer) { delete writer; }

int skydds_reader_open(const skydds_common_options_t* opts, skydds_reader_t** out, char* err, int err_len) {
    if (!opts || !out) return -1;
    if (init_factory_once(opts, err, err_len) != 0) return -1;
    auto* h = new skydds_reader_t();

    h->domain = TheParticipantFactory->create_participant(opts->domain_id, PARTICIPANT_QOS_DEFAULT, 0, SkyDDS::DCPS::DEFAULT_STATUS_MASK);
    if (!h->domain) { set_error(err, err_len, "create_participant failed"); delete h; return -1; }

    Satellite::OctetMsgTypeSupport_var ts = new Satellite::OctetMsgTypeSupportImpl;
    if (ts->register_type(h->domain, "Satellite::OctetMsg") != DDS::RETCODE_OK) {
        set_error(err, err_len, "register_type Satellite::OctetMsg failed"); delete h; return -1;
    }

    h->topic = h->domain->create_topic(opts->topic_name, "Satellite::OctetMsg", TOPIC_QOS_DEFAULT, 0, SkyDDS::DCPS::DEFAULT_STATUS_MASK);
    if (!h->topic) { set_error(err, err_len, "create_topic failed"); delete h; return -1; }

    h->sub = h->domain->create_subscriber(SUBSCRIBER_QOS_DEFAULT, 0, SkyDDS::DCPS::DEFAULT_STATUS_MASK);
    if (!h->sub) { set_error(err, err_len, "create_subscriber failed"); delete h; return -1; }

    h->listener = new OctetReaderListener();
    h->dr = h->sub->create_datareader(h->topic, DATAREADER_QOS_DEFAULT, h->listener, SkyDDS::DCPS::DEFAULT_STATUS_MASK);
    if (!h->dr) { set_error(err, err_len, "create_datareader failed"); delete h->listener; delete h; return -1; }

    h->reader = Satellite::OctetMsgDataReader::_narrow(h->dr);
    if (!h->reader) { set_error(err, err_len, "_narrow OctetMsgDataReader failed"); delete h->listener; delete h; return -1; }

    *out = h;
    return 0;
}

int skydds_reader_poll(skydds_reader_t* reader, uint8_t* out_buf, int out_cap, int timeout_ms, int* out_len, char* err, int err_len) {
    (void)timeout_ms;
    if (!reader || !reader->listener || !out_len) return -1;
    *out_len = 0;

    ACE_GUARD_RETURN(ACE_Thread_Mutex, guard, reader->listener->lock_, -1);
    if (reader->listener->queue_.empty()) {
        return 1; // timeout/no data
    }
    std::vector<uint8_t> msg = std::move(reader->listener->queue_.front());
    reader->listener->queue_.pop_front();
    if (static_cast<int>(msg.size()) > out_cap) {
        set_error(err, err_len, "receiver output buffer too small");
        return -2;
    }
    std::memcpy(out_buf, msg.data(), msg.size());
    *out_len = static_cast<int>(msg.size());
    return 0;
}

void skydds_reader_close(skydds_reader_t* reader) {
    if (!reader) return;
    delete reader->listener;
    delete reader;
}
