bind_socket = "127.0.0.1:11334";
# No secure_ip: the loopback namespace is shared with other services, so every
# learner, Thura and the supervisor's health check included, presents the password.
.include "/tmp/controller-secret.inc"
allow_file_and_shm_inputs = false;
