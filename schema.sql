-- =============================================================================
-- NazeerDFS Distributed File Storage System Database Schema
-- Compatible with MySQL 5.7+, MySQL 8.0+, MariaDB, phpMyAdmin
-- =============================================================================

-- -----------------------------------------------------------------------------
-- 1. USERS & AUTHENTICATION TABLE
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `users` (
    `id` VARCHAR(36) PRIMARY KEY,
    `username` VARCHAR(50) NOT NULL UNIQUE,
    `password_hash` VARCHAR(255) NOT NULL,
    `full_name` VARCHAR(100) NOT NULL,
    `role` VARCHAR(30) NOT NULL DEFAULT 'User',
    `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX `idx_users_username` (`username`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- -----------------------------------------------------------------------------
-- 2. CLUSTER NODES TABLE
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `nodes` (
    `id` VARCHAR(100) PRIMARY KEY,
    `name` VARCHAR(100) NOT NULL,
    `address` VARCHAR(100) NOT NULL,
    `role` VARCHAR(50) NOT NULL DEFAULT 'Storage Peer',
    `status` VARCHAR(30) NOT NULL DEFAULT 'Online',
    `total_bytes` BIGINT NOT NULL DEFAULT 4398046511104,
    `used_bytes` BIGINT NOT NULL DEFAULT 0,
    `last_heartbeat` TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
    `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX `idx_nodes_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- -----------------------------------------------------------------------------
-- 3. FILES METADATA TABLE
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `files` (
    `id` VARCHAR(36) PRIMARY KEY,
    `file_key` VARCHAR(64) NOT NULL UNIQUE,
    `filename` VARCHAR(255) NOT NULL,
    `file_size` BIGINT NOT NULL,
    `content_type` VARCHAR(100) DEFAULT 'application/octet-stream',
    `owner_id` VARCHAR(36) NULL,
    `owner_name` VARCHAR(100) NOT NULL DEFAULT 'System Admin',
    `checksum` VARCHAR(64) NOT NULL,
    `encryption_algorithm` VARCHAR(50) NOT NULL DEFAULT 'AES-256-GCM',
    `status` VARCHAR(30) NOT NULL DEFAULT 'Healthy',
    `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX `idx_files_key` (`file_key`),
    INDEX `idx_files_status` (`status`),
    INDEX `idx_files_owner` (`owner_id`),
    CONSTRAINT `fk_files_owner` FOREIGN KEY (`owner_id`) REFERENCES `users` (`id`) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- -----------------------------------------------------------------------------
-- 4. FILE CHUNKS & REPLICAS MAPPING TABLE
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `file_replicas` (
    `id` VARCHAR(36) PRIMARY KEY,
    `file_id` VARCHAR(36) NOT NULL,
    `node_id` VARCHAR(100) NOT NULL,
    `chunk_hash` VARCHAR(64) NOT NULL,
    `chunk_index` INT NOT NULL DEFAULT 0,
    `chunk_size` BIGINT NOT NULL,
    `status` VARCHAR(30) NOT NULL DEFAULT 'Active',
    `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX `idx_replicas_file_id` (`file_id`),
    INDEX `idx_replicas_node_id` (`node_id`),
    UNIQUE KEY `unique_file_node_chunk` (`file_id`, `node_id`, `chunk_index`),
    CONSTRAINT `fk_replicas_file` FOREIGN KEY (`file_id`) REFERENCES `files` (`id`) ON DELETE CASCADE,
    CONSTRAINT `fk_replicas_node` FOREIGN KEY (`node_id`) REFERENCES `nodes` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- -----------------------------------------------------------------------------
-- 5. SYSTEM & CLUSTER AUDIT LOGS TABLE
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `system_logs` (
    `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
    `level` VARCHAR(20) NOT NULL DEFAULT 'INFO',
    `node_id` VARCHAR(100) NULL,
    `message` TEXT NOT NULL,
    `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX `idx_logs_created_at` (`created_at`),
    INDEX `idx_logs_level` (`level`),
    CONSTRAINT `fk_logs_node` FOREIGN KEY (`node_id`) REFERENCES `nodes` (`id`) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- -----------------------------------------------------------------------------
-- 6. ACTIVE USER SESSIONS TABLE
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `sessions` (
    `token` VARCHAR(128) PRIMARY KEY,
    `user_id` VARCHAR(36) NOT NULL,
    `expires_at` TIMESTAMP NOT NULL,
    `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX `idx_sessions_expires_at` (`expires_at`),
    CONSTRAINT `fk_sessions_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- -----------------------------------------------------------------------------
-- INITIAL SEED DATA
-- -----------------------------------------------------------------------------

-- Default Admin User (Password hash of 'admin123' matching NazeerDFS SHA-256)
INSERT IGNORE INTO `users` (`id`, `username`, `password_hash`, `full_name`, `role`)
VALUES (
    'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11',
    'admin',
    '240be518fabd2724ddb6f04eeb1da5967448d7e831c08c8fa822809f74c720a9',
    'Administrator',
    'Cluster Admin'
);

-- Initial 3 Cluster Nodes
INSERT IGNORE INTO `nodes` (`id`, `name`, `address`, `role`, `status`)
VALUES
    ('node-3000', 'Node 3000 (Bootstrap)', ':3000', 'Bootstrap Leader', 'Online'),
    ('node-4000', 'Node 4000 (Peer)', ':4000', 'Storage Peer', 'Online'),
    ('node-5000', 'Node 5000 (Peer)', ':5000', 'Storage Peer', 'Online');

-- Initial System Audit Log
INSERT INTO `system_logs` (`level`, `node_id`, `message`)
VALUES
    ('INFO', 'node-3000', 'NazeerDFS cluster MySQL database schema initialized successfully.'),
    ('INFO', 'node-3000', 'Bootstrap leader listening on port :3000.'),
    ('INFO', 'node-4000', 'Storage peer connected on port :4000.'),
    ('INFO', 'node-5000', 'Storage peer connected on port :5000.');
