|scenario|type|proto|model|pipeline|payload|workers|formal_runs|max_zero_loss_pps_avg|max_zero_loss_mbps_avg|pps_per_worker|judgement|
|---|---|---|---|---|---:|---:|---:|---:|---:|---:|---|
|udp_fastpath_empty_p512_w2|key|udp|fastpath|empty|512|2|3|16000.13|65.54|8000|all formal runs loss_rate == 0|
|udp_pool_empty_p512_w2|key|udp|pool|empty|512|2|3|8001.29|32.77|4000|all formal runs loss_rate == 0|
|udp_channel_empty_p512_w2|key|udp|channel|empty|512|2|3|16000.48|65.54|8000|all formal runs loss_rate == 0|
|tcp_fastpath_empty_p512_w2|key|tcp|fastpath|empty|512|2|3|23999.90|98.30|12000|all formal runs loss_rate == 0|
|tcp_pool_empty_p512_w2|key|tcp|pool|empty|512|2|3|24000.95|98.31|12000|all formal runs loss_rate == 0|
|tcp_channel_empty_p512_w2|key|tcp|channel|empty|512|2|3|24000.78|98.31|12000|all formal runs loss_rate == 0|
|udp_pool_basic_p512_w2|pipeline|udp|pool|basic|512|2|1|16000.13|65.54|8000|all formal runs loss_rate == 0|
|tcp_pool_basic_p512_w2|pipeline|tcp|pool|basic|512|2|1|24000.76|98.31|12000|all formal runs loss_rate == 0|
|udp_pool_complex_p512_w2|pipeline|udp|pool|complex|512|2|1|15999.94|65.54|8000|all formal runs loss_rate == 0|
|tcp_pool_complex_p512_w2|pipeline|tcp|pool|complex|512|2|1|23999.00|98.30|12000|all formal runs loss_rate == 0|
|udp_pool_empty_p256_w2|payload|udp|pool|empty|256|2|1|N/A|N/A|N/A|not_reached|
|udp_pool_empty_p4096_w2|payload|udp|pool|empty|4096|2|1|2000.01|65.54|1000|all formal runs loss_rate == 0|
|tcp_pool_empty_p256_w2|payload|tcp|pool|empty|256|2|1|N/A|N/A|N/A|not_reached|
|tcp_pool_empty_p4096_w2|payload|tcp|pool|empty|4096|2|1|N/A|N/A|N/A|not_reached|
|udp_pool_empty_p512_w1|workers|udp|pool|empty|512|1|1|8000.27|32.77|8000|all formal runs loss_rate == 0|
|udp_pool_empty_p512_w4|workers|udp|pool|empty|512|4|1|8000.29|32.77|2000|all formal runs loss_rate == 0|
|tcp_pool_empty_p512_w1|workers|tcp|pool|empty|512|1|1|N/A|N/A|N/A|not_reached|
|tcp_pool_empty_p512_w4|workers|tcp|pool|empty|512|4|1|N/A|N/A|N/A|not_reached|
