"""
Vulnerable TCP Server for Socket Programming Education
======================================================
A multi-service TCP server that students can connect to and interact with.
Practice: Socket programming, protocol analysis, buffer overflow concepts.

Services:
- Port 9000: Echo service (with overflow)
- Port 9001: Simple auth service (crack the password)
- Port 9002: Chat service (message injection)

WARNING: This server is INTENTIONALLY VULNERABLE. Never expose it to the internet.
"""
import socket
import threading
import hashlib
import os

HOST = "0.0.0.0"

# Hidden credentials for the auth challenge
AUTH_SECRET = "s3cr3t_p4ssw0rd"

def echo_service(client, addr):
    """Simple echo service with buffer overflow simulation."""
    try:
        client.send(b"=== Echo Service ===\r\n")
        client.send(b"Type something and I'll echo it back.\r\n")
        client.send(b"Type 'quit' to disconnect.\r\n\r\n")
        
        while True:
            data = client.recv(1024)
            if not data:
                break
            
            text = data.decode('utf-8', errors='replace').strip()
            
            if text.lower() == 'quit':
                client.send(b"Goodbye!\r\n")
                break
            
            # VULNERABILITY: Buffer overflow simulation
            # If input exceeds 256 bytes, we "overflow" and reveal memory
            if len(data) > 256:
                client.send(b"\r\n*** BUFFER OVERFLOW DETECTED ***\r\n")
                client.send(b"Memory dump:\r\n")
                # Simulate leaked memory
                leaked = os.urandom(32).hex()
                client.send(f"Stack: {leaked}\r\n".encode())
                client.send(f"Flag: FLAG{{buffer_overflow_{leaked[:8]}}}\r\n".encode())
                client.send(b"*** CONNECTION RESET ***\r\n\r\n")
            else:
                client.send(f"Echo: {text}\r\n".encode())
    except Exception as e:
        pass
    finally:
        client.close()

def auth_service(client, addr):
    """Authentication challenge — crack the weak hash."""
    try:
        client.send(b"=== Auth Service ===\r\n")
        client.send(b"Prove you know the secret password.\r\n")
        client.send(b"Send the MD5 hash of the password.\r\n")
        client.send(b"Hint: The password is 12 characters or less, lowercase letters and numbers.\r\n\r\n")
        
        client.send(b"MD5 hash: ")
        data = client.recv(1024)
        if not data:
            return
        
        guess_hash = data.decode('utf-8', errors='replace').strip()
        real_hash = hashlib.md5(AUTH_SECRET.encode()).hexdigest()
        
        if guess_hash == real_hash:
            client.send(b"\r\n*** ACCESS GRANTED ***\r\n")
            client.send(f"Flag: FLAG{{auth_cracked_{real_hash[:8]}}}\r\n".encode())
            client.send(b"Welcome, authenticated user!\r\n")
        else:
            client.send(b"\r\n*** ACCESS DENIED ***\r\n")
            client.send(b"Try again.\r\n")
    except Exception as e:
        pass
    finally:
        client.close()

def chat_service(client, addr):
    """Chat service with message injection vulnerability."""
    try:
        client.send(b"=== Chat Service ===\r\n")
        client.send(b"Welcome to the chat room!\r\n")
        client.send(b"Commands: /name <name> | /msg <text> | /admin <cmd> | /quit\r\n\r\n")
        
        username = f"user_{addr[1]}"
        
        while True:
            data = client.recv(1024)
            if not data:
                break
            
            text = data.decode('utf-8', errors='replace').strip()
            
            if text.startswith("/quit"):
                client.send(b"Goodbye!\r\n")
                break
            elif text.startswith("/name "):
                username = text[6:].strip()[:20]
                client.send(f"Name set to: {username}\r\n".encode())
            elif text.startswith("/admin "):
                # VULNERABILITY: No auth check on admin commands
                cmd = text[7:].strip()
                if cmd == "status":
                    client.send(b"Server: OK | Uptime: 9999h | Users: 1\r\n")
                    client.send(f"Flag: FLAG{{admin_chat_access}}\r\n".encode())
                elif cmd == "users":
                    client.send(b"Connected users: you (and maybe others)\r\n")
                else:
                    client.send(f"Unknown admin command: {cmd}\r\n".encode())
            elif text.startswith("/msg "):
                msg = text[5:].strip()
                # VULNERABILITY: Message injection — CRLF not sanitized
                # An attacker could inject fake messages from other users
                broadcast = f"[{username}]: {msg}\r\n"
                client.send(broadcast.encode())
            else:
                client.send(f"[{username}]: {text}\r\n".encode())
    except Exception as e:
        pass
    finally:
        client.close()

def handle_client(client, addr):
    """Route client to a service based on initial selection."""
    try:
        client.send(b"=== Modigo TCP Challenge Server ===\r\n")
        client.send(b"Select a service:\r\n")
        client.send(b"  1. Echo Service (port 9000 - try to overflow the buffer)\r\n")
        client.send(b"  2. Auth Service (crack the MD5 hash)\r\n")
        client.send(b"  3. Chat Service (find the hidden admin command)\r\n")
        client.send(b"\r\nEnter choice (1/2/3): ")
        
        data = client.recv(1024)
        if not data:
            return
        
        choice = data.decode('utf-8', errors='replace').strip()
        
        if choice == "1":
            echo_service(client, addr)
        elif choice == "2":
            auth_service(client, addr)
        elif choice == "3":
            chat_service(client, addr)
        else:
            client.send(b"Invalid choice.\r\n")
            client.close()
    except Exception as e:
        pass

def main():
    server = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    server.bind((HOST, 9000))
    server.listen(10)
    
    print(f"🔌 TCP Challenge Server listening on {HOST}:9000")
    print("   Services: Echo (overflow), Auth (hash cracking), Chat (admin)")
    print("   This server is INTENTIONALLY VULNERABLE.")
    
    while True:
        client, addr = server.accept()
        t = threading.Thread(target=handle_client, args=(client, addr))
        t.daemon = True
        t.start()

if __name__ == "__main__":
    main()
