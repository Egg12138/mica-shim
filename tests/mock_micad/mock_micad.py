#!/usr/bin/env python3

import os
import sys
import socket
import struct
import signal
import threading
import time
import argparse
from pathlib import Path

# Constants
SOCKET_PATH = "/tmp/mica/mica-create.socket"
BUFFER_SIZE = 1024
MAX_NAME_LEN = 32
RESPONSE_SUCCESS = b"MICA-SUCCESS\n"
RESPONSE_FAILED = b"MICA-FAILED\n"

# Global state
is_running = True
send_response = True
listeners = []
listener_lock = threading.Lock()

class CreateMsg:
    """Message format matching mica.py's CreateMsg"""
    # struct format: I = uint32, 32s = 32 bytes string, 128s = 128 bytes string, 32s, 128s, ? = bool
    FORMAT = '<I32s128s32s128s?'
    SIZE = struct.calcsize(FORMAT)
    
    def __init__(self, data=None):
        if data and len(data) >= self.SIZE:
            unpacked = struct.unpack(self.FORMAT, data[:self.SIZE])
            self.cpu = unpacked[0]
            self.name = unpacked[1].rstrip(b'\x00').decode('utf-8', errors='replace')
            self.path = unpacked[2].rstrip(b'\x00').decode('utf-8', errors='replace')
            self.ped = unpacked[3].rstrip(b'\x00').decode('utf-8', errors='replace')
            self.ped_cfg = unpacked[4].rstrip(b'\x00').decode('utf-8', errors='replace')
            self.debug = unpacked[5]
        else:
            self.cpu = 0
            self.name = ""
            self.path = ""
            self.ped = ""
            self.ped_cfg = ""
            self.debug = False
    
    def __str__(self):
        return f"""
Received Create Message:
CPU: {self.cpu}
Name: {self.name}
Path: {self.path}
Ped: {self.ped}
PedCfg: {self.ped_cfg}
Debug: {self.debug}
"""

def signal_handler(signum, frame):
    """Handle SIGINT and SIGTERM"""
    global is_running
    print(f"\nReceived signal {signum}, shutting down...")
    is_running = False

def print_hex_dump(data):
    """Print data as hex dump"""
    print(f"\nReceived data ({len(data)} bytes):")
    for i in range(0, len(data), 16):
        chunk = data[i:i+16]
        hex_str = ' '.join(f'{b:02x}' for b in chunk)
        print(f"{hex_str}")
    print()

def print_as_string(data):
    """Print data as string with non-printable characters shown as hex"""
    print("Received input as string: ", end="")
    for byte in data:
        if 32 <= byte <= 126:  # printable ASCII
            print(chr(byte), end="")
        elif byte == 0:  # null terminator
            print("*", end="")
        else:  # other non-printable
            print(f"\\x{byte:02x}", end="")
    print()

def safe_send(client_sock, message):
    """Send message safely with error handling"""
    try:
        client_sock.sendall(message)
        return True
    except Exception as e:
        print(f"Send failed: {e}")
        return False

def handle_client(client_sock, client_addr):
    """Handle individual client connection"""
    try:
        data = client_sock.recv(BUFFER_SIZE)
        if not data:
            return
        
        print_hex_dump(data)
        print_as_string(data)
        
        if len(data) == CreateMsg.SIZE:
            # Binary create message
            msg = CreateMsg(data)
            print(msg)
            
            if send_response:
                safe_send(client_sock, RESPONSE_SUCCESS)
        else:
            # Text control message
            try:
                text_msg = data.decode('utf-8', errors='replace').strip()
                print(f"Received control message: {text_msg}")
            except:
                print("Received non-text control message")
            
            if send_response:
                safe_send(client_sock, RESPONSE_SUCCESS)
                
    except Exception as e:
        print(f"Error handling client: {e}")
        if send_response:
            safe_send(client_sock, RESPONSE_FAILED)

def setup_socket(socket_path):
    """Setup Unix domain socket"""
    # Remove existing socket
    try:
        os.unlink(socket_path)
    except FileNotFoundError:
        pass
    
    # Create directory if it doesn't exist
    socket_dir = os.path.dirname(socket_path)
    Path(socket_dir).mkdir(parents=True, exist_ok=True, mode=0o755)
    
    # Create socket
    server_sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    server_sock.bind(socket_path)
    server_sock.listen(10)  # MAX_CLIENTS
    
    return server_sock

class ListenerUnit:
    """Listener unit for handling connections"""
    def __init__(self, name, socket_path):
        self.name = name
        self.socket_path = socket_path
        self.socket = setup_socket(socket_path)
        self.thread = None
        self.running = True
    
    def start(self):
        """Start listener thread"""
        self.thread = threading.Thread(target=self.listen_loop, daemon=True)
        self.thread.start()
    
    def listen_loop(self):
        """Main listening loop"""
        print(f"Listener '{self.name}' started on {self.socket_path}")
        
        while is_running and self.running:
            try:
                # Set timeout to allow periodic checking of is_running
                self.socket.settimeout(1.0)
                client_sock, client_addr = self.socket.accept()
                
                # Handle client in separate thread
                client_thread = threading.Thread(
                    target=handle_client, 
                    args=(client_sock, client_addr),
                    daemon=True
                )
                client_thread.start()
                
                # Clean up client socket after handling
                def cleanup_client():
                    client_thread.join()
                    try:
                        client_sock.close()
                    except:
                        pass
                
                threading.Thread(target=cleanup_client, daemon=True).start()
                
            except socket.timeout:
                continue
            except Exception as e:
                if is_running:
                    print(f"Accept error: {e}")
                break
    
    def stop(self):
        """Stop listener"""
        self.running = False
        try:
            self.socket.close()
            os.unlink(self.socket_path)
        except:
            pass
        
        if self.thread:
            self.thread.join(timeout=2)

def add_listener(name, socket_path):
    """Add a new listener"""
    try:
        listener = ListenerUnit(name, socket_path)
        
        with listener_lock:
            listeners.append(listener)
        
        listener.start()
        return True
    except Exception as e:
        print(f"Failed to add listener: {e}")
        return False

def cleanup_listeners():
    """Clean up all listeners"""
    with listener_lock:
        for listener in listeners:
            listener.stop()
        listeners.clear()

def main():
    global send_response
    
    # Parse command line arguments
    parser = argparse.ArgumentParser(description='Mock MICA daemon')
    parser.add_argument('-q', '--quiet', action='store_true', 
                       help='Do not send response to client')
    args = parser.parse_args()
    
    send_response = not args.quiet
    
    # Setup signal handlers
    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)
    
    # Add main listener
    if not add_listener("mica-create", SOCKET_PATH):
        print("Failed to add listener")
        return 1
    
    print(f"Mock micad started. Listening on {SOCKET_PATH}")
    print("Press Ctrl+C to stop")
    print(f"Response mode: {'enabled' if send_response else 'disabled'}")
    
    # Main loop
    try:
        while is_running:
            time.sleep(1)
    except KeyboardInterrupt:
        print("\nShutdown requested...")
    
    # Cleanup
    cleanup_listeners()
    print("Mock micad stopped.")
    
    return 0

if __name__ == "__main__":
    sys.exit(main()) 