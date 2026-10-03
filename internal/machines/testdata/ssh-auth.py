# Opt-in SSH authentication fixture only; never part of the app or remote helper.
# Requires Paramiko 4.0.0 in a test-local Python environment.
import os, socket, sys, threading, time
import paramiko
root = sys.argv[1]
os.makedirs(root, mode=0o700, exist_ok=True)
host = paramiko.RSAKey.generate(3072)
client = paramiko.RSAKey.generate(3072)
client.write_private_key_file(os.path.join(root, 'client.plain'))
client.write_private_key_file(os.path.join(root, 'client.encrypted'), password='fixture-passphrase')
for name in ('client.plain', 'client.encrypted'):
    os.chmod(os.path.join(root, name), 0o600)
class Server(paramiko.ServerInterface):
    def get_allowed_auths(self, username): return 'publickey,password'
    def check_auth_password(self, username, password):
        return paramiko.AUTH_SUCCESSFUL if username == 'fixture' and password == 'fixture-password' else paramiko.AUTH_FAILED
    def check_auth_publickey(self, username, key):
        return paramiko.AUTH_SUCCESSFUL if username == 'fixture' and key == client else paramiko.AUTH_FAILED
    def check_channel_request(self, kind, channel_id):
        return paramiko.OPEN_SUCCEEDED if kind == 'session' else paramiko.OPEN_FAILED_ADMINISTRATIVELY_PROHIBITED
    def check_channel_exec_request(self, channel, command):
        def reply():
            if command == b'printf auth-proof':
                channel.sendall(b'AUTH_OK\n'); channel.send_exit_status(0)
            else: channel.send_exit_status(1)
            channel.close()
        threading.Thread(target=reply, daemon=True).start()
        return True
listener = socket.socket()
listener.bind(('127.0.0.1', 0)); listener.listen()
print(listener.getsockname()[1], flush=True)
def accept(conn):
    transport = paramiko.Transport(conn)
    try:
        transport.add_server_key(host); transport.start_server(server=Server())
        while transport.is_active(): time.sleep(.05)
    except (EOFError, OSError, paramiko.SSHException): pass
    finally: transport.close()
while True:
    conn, _ = listener.accept()
    threading.Thread(target=accept, args=(conn,), daemon=True).start()
