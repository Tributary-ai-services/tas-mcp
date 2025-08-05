-- PostgreSQL MCP Server - Sample Database Initialization
-- This script creates sample tables and data for testing PostgreSQL MCP integration

-- Enable necessary extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- =============================================================================
-- SAMPLE SCHEMA: E-COMMERCE APPLICATION
-- =============================================================================

-- Users table
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    uuid UUID DEFAULT uuid_generate_v4() UNIQUE NOT NULL,
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(100) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    first_name VARCHAR(50),
    last_name VARCHAR(50),
    phone VARCHAR(20),
    date_of_birth DATE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    is_active BOOLEAN DEFAULT true,
    last_login TIMESTAMP
);

-- User addresses
CREATE TABLE user_addresses (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    address_type VARCHAR(20) DEFAULT 'shipping', -- shipping, billing
    street_address VARCHAR(255) NOT NULL,
    city VARCHAR(100) NOT NULL,
    state VARCHAR(50),
    postal_code VARCHAR(20),
    country VARCHAR(50) DEFAULT 'USA',
    is_default BOOLEAN DEFAULT false,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Product categories
CREATE TABLE categories (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL,
    description TEXT,
    parent_id INTEGER REFERENCES categories(id),
    slug VARCHAR(100) UNIQUE NOT NULL,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Products table
CREATE TABLE products (
    id SERIAL PRIMARY KEY,
    uuid UUID DEFAULT uuid_generate_v4() UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    sku VARCHAR(100) UNIQUE NOT NULL,
    category_id INTEGER REFERENCES categories(id),
    price DECIMAL(10,2) NOT NULL,
    cost DECIMAL(10,2),
    weight DECIMAL(8,3),
    dimensions JSONB, -- {width, height, depth, unit}
    stock_quantity INTEGER DEFAULT 0,
    min_stock_level INTEGER DEFAULT 5,
    is_active BOOLEAN DEFAULT true,
    featured BOOLEAN DEFAULT false,
    tags TEXT[],
    metadata JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Product images
CREATE TABLE product_images (
    id SERIAL PRIMARY KEY,
    product_id INTEGER REFERENCES products(id) ON DELETE CASCADE,
    image_url VARCHAR(500) NOT NULL,
    alt_text VARCHAR(255),
    sort_order INTEGER DEFAULT 0,
    is_primary BOOLEAN DEFAULT false
);

-- Orders table
CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    uuid UUID DEFAULT uuid_generate_v4() UNIQUE NOT NULL,
    user_id INTEGER REFERENCES users(id),
    order_number VARCHAR(50) UNIQUE NOT NULL,
    status VARCHAR(20) DEFAULT 'pending', -- pending, processing, shipped, delivered, cancelled
    total_amount DECIMAL(10,2) NOT NULL,
    tax_amount DECIMAL(10,2) DEFAULT 0,
    shipping_amount DECIMAL(10,2) DEFAULT 0,
    discount_amount DECIMAL(10,2) DEFAULT 0,
    payment_status VARCHAR(20) DEFAULT 'pending', -- pending, paid, failed, refunded
    payment_method VARCHAR(50),
    shipping_address JSONB,
    billing_address JSONB,
    notes TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    shipped_at TIMESTAMP,
    delivered_at TIMESTAMP
);

-- Order items
CREATE TABLE order_items (
    id SERIAL PRIMARY KEY,
    order_id INTEGER REFERENCES orders(id) ON DELETE CASCADE,
    product_id INTEGER REFERENCES products(id),
    quantity INTEGER NOT NULL,
    unit_price DECIMAL(10,2) NOT NULL,
    total_price DECIMAL(10,2) NOT NULL,
    product_snapshot JSONB -- Store product details at time of order
);

-- Inventory tracking
CREATE TABLE inventory_transactions (
    id SERIAL PRIMARY KEY,
    product_id INTEGER REFERENCES products(id),
    transaction_type VARCHAR(20) NOT NULL, -- purchase, sale, adjustment, return
    quantity_change INTEGER NOT NULL,
    previous_quantity INTEGER NOT NULL,
    new_quantity INTEGER NOT NULL,
    reference_id INTEGER, -- order_id, purchase_id, etc.
    reference_type VARCHAR(50), -- order, purchase, adjustment
    notes TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_by INTEGER REFERENCES users(id)
);

-- Customer reviews
CREATE TABLE reviews (
    id SERIAL PRIMARY KEY,
    product_id INTEGER REFERENCES products(id),
    user_id INTEGER REFERENCES users(id),
    order_id INTEGER REFERENCES orders(id),
    rating INTEGER CHECK (rating >= 1 AND rating <= 5),
    title VARCHAR(255),
    comment TEXT,
    is_verified_purchase BOOLEAN DEFAULT false,
    is_published BOOLEAN DEFAULT true,
    helpful_votes INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- =============================================================================
-- INDEXES FOR PERFORMANCE
-- =============================================================================

-- Users indexes
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_created_at ON users(created_at);
CREATE INDEX idx_users_active ON users(is_active);

-- Products indexes
CREATE INDEX idx_products_category_id ON products(category_id);
CREATE INDEX idx_products_sku ON products(sku);
CREATE INDEX idx_products_price ON products(price);
CREATE INDEX idx_products_stock ON products(stock_quantity);
CREATE INDEX idx_products_active ON products(is_active);
CREATE INDEX idx_products_featured ON products(featured);
CREATE INDEX idx_products_tags ON products USING GIN(tags);
CREATE INDEX idx_products_metadata ON products USING GIN(metadata);

-- Orders indexes
CREATE INDEX idx_orders_user_id ON orders(user_id);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_created_at ON orders(created_at);
CREATE INDEX idx_orders_order_number ON orders(order_number);

-- Order items indexes
CREATE INDEX idx_order_items_order_id ON order_items(order_id);
CREATE INDEX idx_order_items_product_id ON order_items(product_id);

-- Reviews indexes
CREATE INDEX idx_reviews_product_id ON reviews(product_id);
CREATE INDEX idx_reviews_user_id ON reviews(user_id);
CREATE INDEX idx_reviews_rating ON reviews(rating);
CREATE INDEX idx_reviews_published ON reviews(is_published);

-- =============================================================================
-- SAMPLE DATA INSERTION
-- =============================================================================

-- Insert sample categories
INSERT INTO categories (name, description, slug) VALUES
('Electronics', 'Electronic devices and accessories', 'electronics'),
('Computers', 'Laptops, desktops, and computer accessories', 'computers'),
('Mobile Phones', 'Smartphones and mobile accessories', 'mobile-phones'),
('Books', 'Physical and digital books', 'books'),
('Clothing', 'Apparel and fashion items', 'clothing'),
('Home & Garden', 'Home improvement and garden supplies', 'home-garden');

-- Create subcategories
INSERT INTO categories (name, description, slug, parent_id) VALUES
('Laptops', 'Portable computers', 'laptops', 2),
('Smartphones', 'Mobile phones with smart features', 'smartphones', 3),
('Fiction', 'Fictional literature', 'fiction', 4),
('Programming', 'Programming and technical books', 'programming', 4);

-- Insert sample users
INSERT INTO users (username, email, password_hash, first_name, last_name, phone, date_of_birth) VALUES
('john_doe', 'john.doe@example.com', crypt('password123', gen_salt('bf')), 'John', 'Doe', '+1-555-0101', '1985-03-15'),
('jane_smith', 'jane.smith@example.com', crypt('password123', gen_salt('bf')), 'Jane', 'Smith', '+1-555-0102', '1990-07-22'),
('bob_wilson', 'bob.wilson@example.com', crypt('password123', gen_salt('bf')), 'Bob', 'Wilson', '+1-555-0103', '1988-12-03'),
('alice_brown', 'alice.brown@example.com', crypt('password123', gen_salt('bf')), 'Alice', 'Brown', '+1-555-0104', '1992-05-18'),
('charlie_davis', 'charlie.davis@example.com', crypt('password123', gen_salt('bf')), 'Charlie', 'Davis', '+1-555-0105', '1987-09-28');

-- Insert sample addresses
INSERT INTO user_addresses (user_id, address_type, street_address, city, state, postal_code, is_default) VALUES
(1, 'shipping', '123 Main St', 'New York', 'NY', '10001', true),
(1, 'billing', '123 Main St', 'New York', 'NY', '10001', false),
(2, 'shipping', '456 Oak Ave', 'Los Angeles', 'CA', '90210', true),
(3, 'shipping', '789 Pine Rd', 'Chicago', 'IL', '60601', true),
(4, 'shipping', '321 Elm St', 'Houston', 'TX', '77001', true),
(5, 'shipping', '654 Maple Dr', 'Phoenix', 'AZ', '85001', true);

-- Insert sample products
INSERT INTO products (name, description, sku, category_id, price, cost, weight, stock_quantity, featured, tags, metadata) VALUES
('MacBook Pro 16"', 'Apple MacBook Pro with M2 chip, 16-inch display', 'MBP-16-M2-001', 7, 2499.99, 2000.00, 2.1, 15, true, ARRAY['apple', 'laptop', 'premium'], '{"warranty": "1 year", "color": "Space Gray"}'),
('Dell XPS 13', 'Ultra-portable laptop with Intel i7 processor', 'DELL-XPS13-002', 7, 1299.99, 1000.00, 1.2, 25, true, ARRAY['dell', 'laptop', 'portable'], '{"warranty": "2 years", "color": "Silver"}'),
('iPhone 15 Pro', 'Latest iPhone with advanced camera system', 'IPHONE-15-PRO-003', 8, 999.99, 700.00, 0.2, 50, true, ARRAY['apple', 'smartphone', 'premium'], '{"storage": "128GB", "color": "Titanium Blue"}'),
('Samsung Galaxy S24', 'Flagship Android smartphone', 'GALAXY-S24-004', 8, 899.99, 650.00, 0.18, 35, false, ARRAY['samsung', 'android', 'smartphone'], '{"storage": "256GB", "color": "Phantom Black"}'),
('The Great Gatsby', 'Classic American novel by F. Scott Fitzgerald', 'BOOK-GATSBY-005', 9, 12.99, 8.00, 0.3, 100, false, ARRAY['fiction', 'classic', 'american'], '{"pages": 180, "publisher": "Scribner"}'),
('Clean Code', 'A handbook of agile software craftsmanship', 'BOOK-CLEAN-006', 10, 39.99, 25.00, 0.8, 75, true, ARRAY['programming', 'software', 'technical'], '{"author": "Robert C. Martin", "pages": 464}'),
('Wireless Earbuds', 'High-quality wireless earbuds with noise cancellation', 'EARBUDS-WL-007', 1, 149.99, 75.00, 0.1, 200, false, ARRAY['audio', 'wireless', 'earbuds'], '{"battery": "6 hours", "charging_case": "24 hours"}'),
('Gaming Mouse', 'High-precision gaming mouse with RGB lighting', 'MOUSE-GAME-008', 2, 79.99, 40.00, 0.15, 80, false, ARRAY['gaming', 'mouse', 'rgb'], '{"dpi": "16000", "buttons": 8}');

-- Insert product images
INSERT INTO product_images (product_id, image_url, alt_text, sort_order, is_primary) VALUES
(1, 'https://example.com/images/macbook-pro-16-1.jpg', 'MacBook Pro 16 inch front view', 1, true),
(1, 'https://example.com/images/macbook-pro-16-2.jpg', 'MacBook Pro 16 inch side view', 2, false),
(2, 'https://example.com/images/dell-xps13-1.jpg', 'Dell XPS 13 laptop', 1, true),
(3, 'https://example.com/images/iphone-15-pro-1.jpg', 'iPhone 15 Pro in Titanium Blue', 1, true),
(4, 'https://example.com/images/galaxy-s24-1.jpg', 'Samsung Galaxy S24', 1, true),
(5, 'https://example.com/images/great-gatsby-cover.jpg', 'The Great Gatsby book cover', 1, true);

-- Insert sample orders
INSERT INTO orders (user_id, order_number, status, total_amount, tax_amount, shipping_amount, payment_status, payment_method, shipping_address, billing_address, notes) VALUES
(1, 'ORD-2025-001', 'delivered', 2649.98, 212.00, 0.00, 'paid', 'credit_card', 
 '{"street": "123 Main St", "city": "New York", "state": "NY", "postal_code": "10001"}',
 '{"street": "123 Main St", "city": "New York", "state": "NY", "postal_code": "10001"}',
 'Express delivery requested'),
(2, 'ORD-2025-002', 'shipped', 1012.98, 81.00, 12.99, 'paid', 'paypal',
 '{"street": "456 Oak Ave", "city": "Los Angeles", "state": "CA", "postal_code": "90210"}',
 '{"street": "456 Oak Ave", "city": "Los Angeles", "state": "CA", "postal_code": "90210"}',
 'Standard shipping'),
(3, 'ORD-2025-003', 'processing', 229.97, 18.40, 9.99, 'paid', 'credit_card',
 '{"street": "789 Pine Rd", "city": "Chicago", "state": "IL", "postal_code": "60601"}',
 '{"street": "789 Pine Rd", "city": "Chicago", "state": "IL", "postal_code": "60601"}',
 NULL),
(4, 'ORD-2025-004', 'pending', 52.98, 4.24, 5.99, 'pending', 'bank_transfer',
 '{"street": "321 Elm St", "city": "Houston", "state": "TX", "postal_code": "77001"}',
 '{"street": "321 Elm St", "city": "Houston", "state": "TX", "postal_code": "77001"}',
 'Payment pending');

-- Insert order items
INSERT INTO order_items (order_id, product_id, quantity, unit_price, total_price, product_snapshot) VALUES
(1, 1, 1, 2499.99, 2499.99, '{"name": "MacBook Pro 16\"", "sku": "MBP-16-M2-001", "color": "Space Gray"}'),
(1, 6, 1, 39.99, 39.99, '{"name": "Clean Code", "sku": "BOOK-CLEAN-006", "author": "Robert C. Martin"}'),
(1, 7, 1, 149.99, 149.99, '{"name": "Wireless Earbuds", "sku": "EARBUDS-WL-007"}'),
(2, 3, 1, 999.99, 999.99, '{"name": "iPhone 15 Pro", "sku": "IPHONE-15-PRO-003", "color": "Titanium Blue", "storage": "128GB"}'),
(2, 5, 1, 12.99, 12.99, '{"name": "The Great Gatsby", "sku": "BOOK-GATSBY-005"}'),
(3, 7, 1, 149.99, 149.99, '{"name": "Wireless Earbuds", "sku": "EARBUDS-WL-007"}'),
(3, 8, 1, 79.99, 79.99, '{"name": "Gaming Mouse", "sku": "MOUSE-GAME-008"}'),
(4, 5, 2, 12.99, 25.98, '{"name": "The Great Gatsby", "sku": "BOOK-GATSBY-005"}'),
(4, 6, 1, 39.99, 39.99, '{"name": "Clean Code", "sku": "BOOK-CLEAN-006"}');

-- Insert inventory transactions
INSERT INTO inventory_transactions (product_id, transaction_type, quantity_change, previous_quantity, new_quantity, reference_id, reference_type, notes) VALUES
(1, 'purchase', 20, 0, 20, NULL, 'initial_stock', 'Initial inventory'),
(1, 'sale', -1, 20, 19, 1, 'order', 'Order ORD-2025-001'),
(3, 'purchase', 60, 0, 60, NULL, 'initial_stock', 'Initial inventory'),
(3, 'sale', -1, 60, 59, 2, 'order', 'Order ORD-2025-002'),
(7, 'purchase', 210, 0, 210, NULL, 'initial_stock', 'Initial inventory'),
(7, 'sale', -2, 210, 208, NULL, 'multiple_orders', 'Orders ORD-2025-001, ORD-2025-003');

-- Insert sample reviews
INSERT INTO reviews (product_id, user_id, order_id, rating, title, comment, is_verified_purchase, helpful_votes) VALUES
(1, 1, 1, 5, 'Excellent laptop!', 'The MacBook Pro 16 is incredibly fast and the display is stunning. Perfect for development work.', true, 12),
(3, 2, 2, 4, 'Great phone, but expensive', 'The iPhone 15 Pro has amazing cameras and performance, but the price is quite high.', true, 8),
(6, 1, 1, 5, 'Must-read for developers', 'Clean Code is essential reading for any serious software developer. Clear and practical advice.', true, 25),
(7, 3, 3, 4, 'Good sound quality', 'The wireless earbuds have great sound quality and good battery life. Comfortable to wear.', true, 5),
(5, 4, 4, 3, 'Classic but dated', 'The Great Gatsby is a classic, but some themes feel dated by modern standards.', true, 3);

-- =============================================================================
-- UPDATE STATISTICS FOR QUERY OPTIMIZATION
-- =============================================================================

-- Update table statistics for better query planning
ANALYZE users;
ANALYZE user_addresses;
ANALYZE categories;
ANALYZE products;
ANALYZE product_images;
ANALYZE orders;
ANALYZE order_items;
ANALYZE inventory_transactions;
ANALYZE reviews;

-- =============================================================================
-- CREATE VIEWS FOR COMMON QUERIES
-- =============================================================================

-- Product summary view
CREATE VIEW product_summary AS
SELECT 
    p.id,
    p.uuid,
    p.name,
    p.sku,
    p.price,
    p.stock_quantity,
    c.name as category_name,
    COALESCE(AVG(r.rating), 0) as avg_rating,
    COUNT(r.id) as review_count,
    p.featured,
    p.is_active,
    p.created_at
FROM products p
LEFT JOIN categories c ON p.category_id = c.id
LEFT JOIN reviews r ON p.id = r.product_id AND r.is_published = true
GROUP BY p.id, p.uuid, p.name, p.sku, p.price, p.stock_quantity, c.name, p.featured, p.is_active, p.created_at;

-- Order summary view
CREATE VIEW order_summary AS
SELECT 
    o.id,
    o.uuid,
    o.order_number,
    o.status,
    o.total_amount,
    o.payment_status,
    u.username,
    u.email,
    COUNT(oi.id) as item_count,
    o.created_at,
    o.updated_at
FROM orders o
LEFT JOIN users u ON o.user_id = u.id
LEFT JOIN order_items oi ON o.id = oi.order_id
GROUP BY o.id, o.uuid, o.order_number, o.status, o.total_amount, o.payment_status, u.username, u.email, o.created_at, o.updated_at;

-- User activity summary
CREATE VIEW user_activity AS
SELECT 
    u.id,
    u.username,
    u.email,
    u.first_name,
    u.last_name,
    COUNT(DISTINCT o.id) as total_orders,
    COALESCE(SUM(o.total_amount), 0) as total_spent,
    COUNT(DISTINCT r.id) as total_reviews,
    u.created_at as joined_date,
    u.last_login
FROM users u
LEFT JOIN orders o ON u.id = o.user_id AND o.status != 'cancelled'
LEFT JOIN reviews r ON u.id = r.user_id
GROUP BY u.id, u.username, u.email, u.first_name, u.last_name, u.created_at, u.last_login;

-- =============================================================================
-- SAMPLE STORED PROCEDURES (FOR TESTING ADVANCED FEATURES)
-- =============================================================================

-- Function to get product analytics
CREATE OR REPLACE FUNCTION get_product_analytics(product_id_param INTEGER)
RETURNS TABLE (
    product_name VARCHAR,
    total_sold INTEGER,
    revenue DECIMAL,
    avg_rating DECIMAL,
    stock_status VARCHAR
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        p.name as product_name,
        COALESCE(SUM(oi.quantity)::INTEGER, 0) as total_sold,
        COALESCE(SUM(oi.total_price), 0) as revenue,
        COALESCE(AVG(r.rating), 0) as avg_rating,
        CASE 
            WHEN p.stock_quantity = 0 THEN 'Out of Stock'
            WHEN p.stock_quantity <= p.min_stock_level THEN 'Low Stock'
            ELSE 'In Stock'
        END as stock_status
    FROM products p
    LEFT JOIN order_items oi ON p.id = oi.product_id
    LEFT JOIN orders o ON oi.order_id = o.id AND o.status != 'cancelled'
    LEFT JOIN reviews r ON p.id = r.product_id AND r.is_published = true
    WHERE p.id = product_id_param
    GROUP BY p.id, p.name, p.stock_quantity, p.min_stock_level;
END;
$$ LANGUAGE plpgsql;

-- =============================================================================
-- COMPLETION MESSAGE
-- =============================================================================

-- Insert a completion record
DO $$
BEGIN
    RAISE NOTICE 'PostgreSQL MCP Server sample database initialized successfully!';
    RAISE NOTICE 'Created % users, % products, % orders, and % reviews', 
        (SELECT COUNT(*) FROM users),
        (SELECT COUNT(*) FROM products), 
        (SELECT COUNT(*) FROM orders),
        (SELECT COUNT(*) FROM reviews);
END $$;