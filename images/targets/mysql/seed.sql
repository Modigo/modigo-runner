-- Modigo sample database — MySQL version
USE modigo;

CREATE TABLE users (
    id         INT AUTO_INCREMENT PRIMARY KEY,
    name       VARCHAR(100) NOT NULL,
    email      VARCHAR(150) UNIQUE NOT NULL,
    age        INT,
    country    VARCHAR(60),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE products (
    id       INT AUTO_INCREMENT PRIMARY KEY,
    name     VARCHAR(100) NOT NULL,
    category VARCHAR(60),
    price    DECIMAL(10,2) NOT NULL,
    stock    INT DEFAULT 0
);

CREATE TABLE orders (
    id         INT AUTO_INCREMENT PRIMARY KEY,
    user_id    INT,
    product_id INT,
    quantity   INT NOT NULL,
    total      DECIMAL(10,2),
    ordered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id)    REFERENCES users(id),
    FOREIGN KEY (product_id) REFERENCES products(id)
);

INSERT INTO users (name, email, age, country) VALUES
    ('Alice Johnson',  'alice@example.com',  29, 'Nigeria'),
    ('Bob Smith',      'bob@example.com',    34, 'Ghana'),
    ('Chidi Okafor',   'chidi@example.com',  22, 'Nigeria'),
    ('Diana Prince',   'diana@example.com',  27, 'Kenya'),
    ('Emeka Nwosu',    'emeka@example.com',  31, 'Nigeria'),
    ('Fatima Al-Said', 'fatima@example.com', 26, 'Egypt'),
    ('George Mensah',  'george@example.com', 38, 'Ghana');

INSERT INTO products (name, category, price, stock) VALUES
    ('Python Masterclass',    'Course',   49.99, 1000),
    ('Go Systems Programming','Course',   59.99,  800),
    ('Linux CLI Handbook',    'Book',     19.99,  500),
    ('Mechanical Keyboard',   'Hardware',120.00,   50),
    ('USB-C Hub',             'Hardware', 35.00,  200),
    ('Rust in Practice',      'Course',   54.99,  600),
    ('PostgreSQL Deep Dive',  'Course',   44.99,  750);

INSERT INTO orders (user_id, product_id, quantity, total) VALUES
    (1,1,1,49.99),(1,3,2,39.98),(2,2,1,59.99),
    (3,1,1,49.99),(3,6,1,54.99),(4,4,1,120.00),
    (5,7,1,44.99),(6,2,1,59.99),(7,5,2,70.00);
