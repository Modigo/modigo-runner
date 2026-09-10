"""
Vulnerable REST API for Cybersecurity Education
================================================
Practice: Auth Bypass, IDOR, JWT Forgery, Mass Assignment, Rate Limit Bypass

WARNING: This API is INTENTIONALLY VULNERABLE. Never expose it to the internet.
"""
from flask import Flask, request, jsonify
import json
import hashlib
import base64
import time

app = Flask(__name__)

# Fake database (in-memory)
users = {
    1: {"id": 1, "username": "admin", "password_hash": hashlib.md5(b"admin").hexdigest(), "role": "admin", "balance": 10000, "email": "admin@vulnapi.local"},
    2: {"id": 2, "username": "alice", "password_hash": hashlib.md5(b"password1").hexdigest(), "role": "user", "balance": 500, "email": "alice@vulnapi.local"},
    3: {"id": 3, "username": "bob", "password_hash": hashlib.md5(b"qwerty").hexdigest(), "role": "user", "balance": 250, "email": "bob@vulnapi.local"},
}

# Fake "JWT" tokens (intentionally weak)
tokens = {}

def create_weak_token(user_id):
    """Create a predictable JWT-like token."""
    payload = {"user_id": user_id, "exp": int(time.time()) + 3600}
    payload_b64 = base64.urlsafe_b64encode(json.dumps(payload).encode()).decode().rstrip("=")
    signature = hashlib.md5(payload_b64.encode()).hexdigest()[:16]
    return f"{payload_b64}.{signature}"

@app.route("/")
def index():
    return jsonify({
        "name": "VulnAPI",
        "description": "Intentionally vulnerable REST API",
        "endpoints": {
            "POST /api/auth/login": "Login (weak hashing)",
            "GET /api/users": "List users (no auth)",
            "GET /api/users/<id>": "Get user (IDOR)",
            "PUT /api/users/<id>": "Update user (mass assignment)",
            "POST /api/transfer": "Transfer funds (no CSRF)",
            "GET /api/admin/users": "Admin: all users (auth bypass)",
            "POST /api/admin/execute": "Admin: command exec (auth bypass)",
        }
    })

@app.route("/api/auth/login", methods=["POST"])
def login():
    data = request.get_json() or {}
    username = data.get("username", "")
    password = data.get("password", "")
    
    # VULNERABILITY: MD5 hashing (easily crackable)
    password_hash = hashlib.md5(password.encode()).hexdigest()
    
    for uid, user in users.items():
        if user["username"] == username and user["password_hash"] == password_hash:
            token = create_weak_token(uid)
            tokens[token] = uid
            return jsonify({
                "token": token,
                "user": {"id": uid, "username": username, "role": user["role"]}
            })
    
    return jsonify({"error": "Invalid credentials"}), 401

@app.route("/api/users")
def list_users():
    # VULNERABILITY: No authentication required
    return jsonify({
        "users": [
            {"id": u["id"], "username": u["username"], "email": u["email"], "balance": u["balance"]}
            for u in users.values()
        ]
    })

@app.route("/api/users/<int:user_id>")
def get_user(user_id):
    # VULNERABILITY: IDOR — no check that the authenticated user owns this profile
    if user_id in users:
        u = users[user_id]
        return jsonify({
            "id": u["id"], "username": u["username"], "email": u["email"],
            "role": u["role"], "balance": u["balance"]
        })
    return jsonify({"error": "User not found"}), 404

@app.route("/api/users/<int:user_id>", methods=["PUT"])
def update_user(user_id):
    # VULNERABILITY: Mass Assignment — can set role, balance, etc.
    data = request.get_json() or {}
    if user_id in users:
        for key, value in data.items():
            if key in ("role", "balance", "email"):  # Mass assignment!
                users[user_id][key] = value
        return jsonify({"message": "Updated", "user": users[user_id]})
    return jsonify({"error": "User not found"}), 404

@app.route("/api/transfer", methods=["POST"])
def transfer():
    data = request.get_json() or {}
    from_id = data.get("from")
    to_id = data.get("to")
    amount = data.get("amount", 0)
    
    # VULNERABILITY: No auth, negative amounts accepted, no balance check
    if from_id in users and to_id in users:
        users[from_id]["balance"] -= amount
        users[to_id]["balance"] += amount
        return jsonify({
            "message": "Transfer successful",
            "flag": "FLAG{insecure_api_transfer}",
            "from_balance": users[from_id]["balance"],
            "to_balance": users[to_id]["balance"]
        })
    return jsonify({"error": "Invalid users"}), 400

@app.route("/api/admin/users")
def admin_users():
    # VULNERABILITY: No auth check — anyone can access admin endpoint
    return jsonify({
        "all_users": [
            {"id": u["id"], "username": u["username"], "password_hash": u["password_hash"], "role": u["role"]}
            for u in users.values()
        ],
        "flag": "FLAG{admin_endpoint_no_auth}"
    })

@app.route("/api/admin/execute", methods=["POST"])
def admin_execute():
    # VULNERABILITY: Command execution with no auth
    data = request.get_json() or {}
    import subprocess
    cmd = data.get("command", "echo 'no command provided'")
    result = subprocess.run(["/bin/sh", "-c", cmd], capture_output=True, text=True, timeout=10)
    return jsonify({
        "stdout": result.stdout,
        "stderr": result.stderr,
        "exit_code": result.returncode
    })

if __name__ == "__main__":
    print("🔓 VulnAPI starting on port 3000...")
    print("   This is an INTENTIONALLY VULNERABLE API.")
    app.run(host="0.0.0.0", port=3000, debug=False)
