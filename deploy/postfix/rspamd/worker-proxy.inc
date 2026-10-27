enabled = true;
bind_socket = "127.0.0.1:11332";
count = 1;
milter = true;
allow_file_and_shm_inputs = false;
upstream "local" {
  default = true;
  self_scan = false;
  hosts = "127.0.0.1:11333";
}
