
from flask import Flask, request, render_template_string, redirect, url_for, jsonify
import sqlite3
import os
import subprocess

app = Flask(__name__)
DB_PATH = "/tmp/users.db"

def init_db():
    conn = sqlite3.connect(DB_PATH)
    c = conn.cursor()
    c.execute('''CREATE TABLE IF NOT EXISTS users (
        id INTEGER PRIMARY KEY,
        username TEXT UNIQUE,
        password TEXT,
        email TEXT,
        role TEXT DEFAULT 'user',
        secret TEXT DEFAULT 'FLAG{you_found_the_secret_data}'
    )''')
    # Insert some users
    try:
        c.executemany("INSERT INTO users (username, password, email, role) VALUES (?, ?, ?, ?)", [
            ("admin", "admin123", "admin@vulnerable.local", "admin"),
            ("alice", "password1", "alice@vulnerable.local", "user"),
            ("bob", "qwerty", "bob@vulnerable.local", "user"),
        ])
        conn.commit()
    except sqlite3.IntegrityError:
        pass
    conn.close()

@app.route("/")
def index():
    return render_template_string("""
    <h1>🏦 VulnBank - Online Banking</h1>
    <p>Welcome to VulnBank. This is an intentionally vulnerable application.</p>
    <ul>
        <li><a href="/login">Login</a></li>
        <li><a href="/search">Search Users</a></li>
        <li><a href="/profile/1">View Profile (IDOR)</a></li>
        <li><a href="/admin">Admin Panel</a></li>
        <li><a href="/ping">Network Tools</a></li>
        <li><a href="/read?file=README">Read File</a></li>
    </ul>
    <h3>Vulnerabilities to Find:</h3>
    <ol>
        <li>SQL Injection on the login page</li>
        <li>SQL Injection on the search page</li>
        <li>Cross-Site Scripting (XSS)</li>
        <li>Command Injection on the ping tool</li>
        <li>Path Traversal on the file reader</li>
        <li>IDOR on user profiles</li>
    </ol>
    """)

@app.route("/login", methods=["GET", "POST"])
def login():
    if request.method == "POST":
        username = request.form.get("username", "")
        password = request.form.get("password", "")
        
        conn = sqlite3.connect(DB_PATH)
        c = conn.cursor()
        query = "SELECT * FROM users WHERE username='%s' AND password='%s'" % (username, password)
        try:
            c.execute(query)
            user = c.fetchone()
        except:
            user = None
        conn.close()
        
        if user:
            return jsonify({
                "success": True,
                "message": "Login successful!",
                "user": {
                    "id": user[0],
                    "username": user[1],
                    "email": user[3],
                    "role": user[4],
                    "secret": user[5]  
                }
            })
        else:
            return jsonify({"success": False, "message": "Invalid credentials"}), 401
    
    return render_template_string("""
    <h2>Login to VulnBank</h2>
    <form method="POST">
        <input name="username" placeholder="Username" required><br>
        <input name="password" type="password" placeholder="Password" required><br>
        <button type="submit">Login</button>
    </form>
    <p><a href="/">← Back</a></p>
    """)

@app.route("/search")
def search():
    query = request.args.get("q", "")
    conn = sqlite3.connect(DB_PATH)
    c = conn.cursor()
    
    sql = "SELECT id, username, email FROM users WHERE username LIKE '%" + query + "%' OR email LIKE '%" + query + "%'"
    try:
        c.execute(sql)
        results = c.fetchall()
    except:
        results = []
    conn.close()
    
    html = "<h2>Search Results for: " + query + "</h2>"
    html += "<table border='1'><tr><th>ID</th><th>Username</th><th>Email</th></tr>"
    for r in results:
        html += "<tr><td>%d</td><td>%s</td><td>%s</td></tr>" % r
    html += "</table>"
    html += '<p><a href="/">← Back</a></p>'
    
    return render_template_string(html)

@app.route("/profile/<int:user_id>")
def profile(user_id):
    conn = sqlite3.connect(DB_PATH)
    c = conn.cursor()
    c.execute("SELECT id, username, email, role, secret FROM users WHERE id=?", (user_id,))
    user = c.fetchone()
    conn.close()
    
    if user:
        return jsonify({
            "id": user[0],
            "username": user[1],
            "email": user[2],
            "role": user[3],
            "secret": user[4]
        })
    return jsonify({"error": "User not found"}), 404

@app.route("/admin")
def admin():
    conn = sqlite3.connect(DB_PATH)
    c = conn.cursor()
    c.execute("SELECT id, username, email, role FROM users")
    users = c.fetchall()
    conn.close()
    
    return jsonify({
        "message": "Welcome to Admin Panel",
        "users": [{"id": u[0], "username": u[1], "email": u[2], "role": u[3]} for u in users],
        "flag": "FLAG{admin_panel_accessed_without_auth}"
    })

@app.route("/ping")
def ping():
    host = request.args.get("host", "127.0.0.1")
    result = subprocess.run(["/bin/sh", "-c", "ping -c 1 " + host], 
                          capture_output=True, text=True, timeout=5)
    return render_template_string("""
    <h2>Network Ping Tool</h2>
    <form>
        <input name="host" value="{{ host }}" placeholder="IP or hostname">
        <button type="submit">Ping</button>
    </form>
    <pre>{{ output }}</pre>
    <p><a href="/">← Back</a></p>
    """, host=host, output=result.stdout + result.stderr)

@app.route("/read")
def read_file():
    filename = request.args.get("file", "README")
    try:
        # Intentionally vulnerable path construction
        filepath = "/app/" + filename
        with open(filepath, "r") as f:
            content = f.read()
        return jsonify({"file": filename, "content": content})
    except FileNotFoundError:
        return jsonify({"error": "File not found"}), 404
    except Exception as e:
        return jsonify({"error": str(e)}), 500

@app.route("/api/transfer", methods=["POST"])
def transfer():
    # VULNERABILITY: No CSRF protection, no auth, amount not validated
    data = request.get_json() or {}
    from_user = data.get("from", "")
    to_user = data.get("to", "")
    amount = data.get("amount", 0)
    
    return jsonify({
        "message": "Transfer successful!",
        "from": from_user,
        "to": to_user,
        "amount": amount,
        "flag": "FLAG{insecure_transaction_completed}"
    })

if __name__ == "__main__":
    init_db()
    print("🏦 VulnBank starting on port 80...")
    print("   This is an INTENTIONALLY VULNERABLE application.")
    print("   Never expose this to the internet.")
    app.run(host="0.0.0.0", port=80, debug=False)
