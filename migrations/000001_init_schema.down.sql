-- NazeerDFS Revert Initial Database Schema Migration
-- Version: 000001_init_schema.down.sql

DROP TABLE IF EXISTS `sessions`;
DROP TABLE IF EXISTS `system_logs`;
DROP TABLE IF EXISTS `file_replicas`;
DROP TABLE IF EXISTS `files`;
DROP TABLE IF EXISTS `nodes`;
DROP TABLE IF EXISTS `users`;
