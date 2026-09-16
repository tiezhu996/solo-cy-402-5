-- 律师案件管理系统 (cylawcase) 初始化脚本：首次启动容器时自动执行
-- 表结构与 GORM AutoMigrate 保持一致（含命名唯一约束），确保幂等。
CREATE TABLE IF NOT EXISTS users (
  id BIGSERIAL PRIMARY KEY,
  username VARCHAR(50) NOT NULL,
  password_hash VARCHAR(100) NOT NULL,
  real_name VARCHAR(50) NOT NULL DEFAULT '',
  role VARCHAR(20) NOT NULL DEFAULT 'lawyer',
  license_no VARCHAR(50) NOT NULL DEFAULT '',
  email VARCHAR(100) NOT NULL DEFAULT '',
  phone VARCHAR(20) NOT NULL DEFAULT '',
  avatar VARCHAR(255) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE users ADD CONSTRAINT uni_users_username UNIQUE (username);

CREATE TABLE IF NOT EXISTS clients (
  id BIGSERIAL PRIMARY KEY,
  name VARCHAR(100) NOT NULL,
  id_number VARCHAR(50) NOT NULL DEFAULT '',
  contact VARCHAR(50) NOT NULL DEFAULT '',
  address VARCHAR(255) NOT NULL DEFAULT '',
  remark TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS cases (
  id BIGSERIAL PRIMARY KEY,
  case_no VARCHAR(50) NOT NULL,
  title VARCHAR(200) NOT NULL,
  case_type VARCHAR(30) NOT NULL DEFAULT 'civil',
  status VARCHAR(30) NOT NULL DEFAULT 'filed',
  accept_date TIMESTAMPTZ,
  close_date TIMESTAMPTZ,
  summary TEXT,
  client_id BIGINT NOT NULL,
  lead_lawyer_id BIGINT NOT NULL,
  co_lawyer_ids JSONB NOT NULL DEFAULT '[]',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE cases ADD CONSTRAINT uni_cases_case_no UNIQUE (case_no);

CREATE TABLE IF NOT EXISTS documents (
  id BIGSERIAL PRIMARY KEY,
  title VARCHAR(200) NOT NULL,
  file_type VARCHAR(30) NOT NULL DEFAULT 'other',
  file_url VARCHAR(500) NOT NULL DEFAULT '',
  upload_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  case_id BIGINT NOT NULL,
  uploader_id BIGINT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS billings (
  id BIGSERIAL PRIMARY KEY,
  bill_no VARCHAR(50) NOT NULL,
  billing_type VARCHAR(30) NOT NULL DEFAULT 'attorney_fee',
  amount NUMERIC(12,2) NOT NULL DEFAULT 0,
  status VARCHAR(30) NOT NULL DEFAULT 'pending',
  case_id BIGINT NOT NULL,
  client_id BIGINT NOT NULL,
  invoice_info VARCHAR(255) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE billings ADD CONSTRAINT uni_billings_bill_no UNIQUE (bill_no);

CREATE TABLE IF NOT EXISTS audit_logs (
  id BIGSERIAL PRIMARY KEY,
  operator_id BIGINT NOT NULL DEFAULT 0,
  operator_name VARCHAR(50) NOT NULL DEFAULT '',
  action VARCHAR(50) NOT NULL,
  entity_type VARCHAR(50) NOT NULL,
  entity_id VARCHAR(50) NOT NULL DEFAULT '',
  detail TEXT,
  ip VARCHAR(50) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 案件当事人档案：本方/对方；冲突检查命中现存未结案件的「对方」档案。
CREATE TABLE IF NOT EXISTS case_parties (
  id BIGSERIAL PRIMARY KEY,
  case_id BIGINT NOT NULL,
  side VARCHAR(20) NOT NULL,
  name VARCHAR(100) NOT NULL,
  id_number VARCHAR(50) NOT NULL DEFAULT '',
  contact VARCHAR(50) NOT NULL DEFAULT '',
  party_role VARCHAR(50) NOT NULL DEFAULT '',
  norm_name VARCHAR(100) NOT NULL,
  norm_id VARCHAR(50) NOT NULL DEFAULT '',
  version BIGINT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- 同一案件同立场，归一化姓名+证件号唯一，避免重复登记。
ALTER TABLE case_parties
  ADD CONSTRAINT idx_case_party_identity UNIQUE (case_id, side, norm_name, norm_id);
CREATE INDEX IF NOT EXISTS idx_case_parties_lookup ON case_parties (side, norm_id, norm_name);

-- 新案案源利益冲突检查记录；唯一业务键建在归一化列上，重复提交收口为唯一行。
CREATE TABLE IF NOT EXISTS conflict_checks (
  id BIGSERIAL PRIMARY KEY,
  check_no VARCHAR(50) NOT NULL,
  case_title VARCHAR(200) NOT NULL DEFAULT '',
  our_parties JSONB NOT NULL DEFAULT '[]',
  opp_name VARCHAR(100) NOT NULL,
  opp_id_number VARCHAR(50) NOT NULL DEFAULT '',
  identity_key VARCHAR(100) NOT NULL,
  norm_opp_name VARCHAR(100) NOT NULL,
  norm_opp_id VARCHAR(50) NOT NULL DEFAULT '',
  status VARCHAR(30) NOT NULL DEFAULT 'pending_review',
  hit_count INT NOT NULL DEFAULT 0,
  matched_snapshot JSONB NOT NULL DEFAULT '[]',
  bound_party_version BIGINT NOT NULL DEFAULT 0,
  bound_fingerprint VARCHAR(64) NOT NULL DEFAULT '',
  review_by_id BIGINT NOT NULL DEFAULT 0,
  review_by_name VARCHAR(50) NOT NULL DEFAULT '',
  review_basis TEXT,
  reviewed_at TIMESTAMPTZ,
  invalidated_reason VARCHAR(200) NOT NULL DEFAULT '',
  submit_by_id BIGINT NOT NULL DEFAULT 0,
  submit_by_name VARCHAR(50) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE conflict_checks ADD CONSTRAINT uni_conflict_check_no UNIQUE (check_no);
-- 关键：同一对方身份（有证件号以证件号为准，否则以姓名为准，均忽略大小写/空白）只有一条结论，承担「唯一终态」收口。
ALTER TABLE conflict_checks
  ADD CONSTRAINT idx_conflict_identity UNIQUE (identity_key);
CREATE INDEX IF NOT EXISTS idx_conflict_status ON conflict_checks (status);
CREATE INDEX IF NOT EXISTS idx_conflict_norm ON conflict_checks (norm_opp_id, norm_opp_name);

-- 冲突结论 ↔ 提交时命中的对方档案版本：关系绑定，档案变化即据此吊销旧放行。
CREATE TABLE IF NOT EXISTS conflict_check_parties (
  id BIGSERIAL PRIMARY KEY,
  check_id BIGINT NOT NULL,
  party_id BIGINT NOT NULL,
  bound_version BIGINT NOT NULL DEFAULT 1,
  bound_fingerprint VARCHAR(64) NOT NULL DEFAULT ''
);
ALTER TABLE conflict_check_parties
  ADD CONSTRAINT idx_check_party UNIQUE (check_id, party_id);
CREATE INDEX IF NOT EXISTS idx_ccp_party ON conflict_check_parties (party_id);

-- 预置种子数据（密码：admin/Admin@123，lawyer 与 assistant/User@123）
INSERT INTO users (id, username, password_hash, real_name, role, license_no, email, phone, avatar, created_at) VALUES
(1, 'admin', '$2a$10$bFfMuQAuKWflKxpuDYdFpeGJPVgD83q/.278LHYLL5S0DDmEfChX2', '系统管理员', 'admin', '', 'admin@cylawcase.dev', '13800000001', '', NOW()),
(2, 'lawyer', '$2a$10$TMTpnDbEwRbtcbF9VJxAxe5IswQjmo7pboKI9zVtU.BYnhzdJpX9a', '张律师', 'lawyer', 'LAW1101010001', 'lawyer@cylawcase.dev', '13800000002', '', NOW()),
(3, 'assistant', '$2a$10$TMTpnDbEwRbtcbF9VJxAxe5IswQjmo7pboKI9zVtU.BYnhzdJpX9a', '李助理', 'assistant', '', 'assistant@cylawcase.dev', '13800000003', '', NOW());

INSERT INTO clients (id, name, id_number, contact, address, remark, created_at) VALUES
(1, '深圳华信科技有限公司', '91440300MA5XXXXX1', '王经理 13900000001', '深圳市南山区科技园', '重点客户', NOW()),
(2, '陈晓明', '440300199001011234', '陈先生 13900000002', '深圳市福田区', '', NOW());

INSERT INTO cases (id, case_no, title, case_type, status, accept_date, close_date, summary, client_id, lead_lawyer_id, co_lawyer_ids, created_at) VALUES
(1, 'CY20260001', '华信科技买卖合同纠纷', 'commercial', 'investigating', NOW() - INTERVAL '15 days', NULL, '货款催收与合同违约赔偿。', 1, 2, '[3]', NOW()),
(2, 'CY20260002', '陈晓明民间借贷纠纷', 'civil', 'filed', NOW() - INTERVAL '6 days', NULL, '借款 50 万元及利息追偿。', 2, 2, '[]', NOW()),
(3, 'CY20260003', '劳动争议仲裁案（已结）', 'labor', 'closed', NOW() - INTERVAL '107 days', NOW() - INTERVAL '27 days', '劳动仲裁已裁决结案。', 2, 2, '[]', NOW());

INSERT INTO documents (id, title, file_type, file_url, upload_time, case_id, uploader_id, created_at) VALUES
(1, '民事起诉状', 'complaint', '/uploads/case1_complaint.pdf', NOW(), 1, 2, NOW()),
(2, '买卖合同证据清单', 'evidence', '/uploads/case1_evidence.pdf', NOW(), 1, 2, NOW()),
(3, '一审判决书', 'judgment', '/uploads/case3_judgment.pdf', NOW(), 3, 2, NOW());

-- 现存未结案件（案件 1、2）的本方与对方档案，供新案利益冲突命中；version 从 1 起。
INSERT INTO case_parties (id, case_id, side, name, id_number, contact, party_role, norm_name, norm_id, version, created_at, updated_at) VALUES
(1, 1, 'our',      '深圳华信科技有限公司', '91440300MA5XXXXX1', '王经理 13900000001', '原告客户', '深圳华信科技有限公司', '91440300MA5XXXXX1', 1, NOW(), NOW()),
(2, 1, 'opposing', '广州恒达物流有限公司', '91440100MA7HDXXX2', '赵总 13700000001',   '被告',     '广州恒达物流有限公司', '91440100MA7HDXXX2', 1, NOW(), NOW()),
(3, 2, 'our',      '陈晓明',             '440300199001011234', '陈先生 13900000002', '原告客户', '陈晓明',             '440300199001011234', 1, NOW(), NOW()),
(4, 2, 'opposing', '王大明',             '440300198505056789', '王先生 13600000002', '被告',     '王大明',             '440300198505056789', 1, NOW(), NOW());

INSERT INTO billings (id, bill_no, billing_type, amount, status, case_id, client_id, invoice_info, created_at) VALUES
(1, 'BILL2026080001', 'attorney_fee', 30000.00, 'paid', 1, 1, '已开票 30000 元', NOW()),
(2, 'BILL2026080002', 'court_fee', 5000.00, 'pending', 1, 1, '', NOW()),
(3, 'BILL2026080003', 'attorney_fee', 15000.00, 'invoiced', 2, 2, '已开票 15000 元', NOW());

INSERT INTO audit_logs (id, operator_id, operator_name, action, entity_type, entity_id, detail, ip, created_at) VALUES
(1, 1, 'admin', 'seed', 'system', '', 'init', '127.0.0.1', NOW());

-- 重置自增序列，避免显式 ID 插入后主键冲突
SELECT setval(pg_get_serial_sequence('users', 'id'), (SELECT COALESCE(MAX(id), 1) FROM users));
SELECT setval(pg_get_serial_sequence('clients', 'id'), (SELECT COALESCE(MAX(id), 1) FROM clients));
SELECT setval(pg_get_serial_sequence('cases', 'id'), (SELECT COALESCE(MAX(id), 1) FROM cases));
SELECT setval(pg_get_serial_sequence('documents', 'id'), (SELECT COALESCE(MAX(id), 1) FROM documents));
SELECT setval(pg_get_serial_sequence('billings', 'id'), (SELECT COALESCE(MAX(id), 1) FROM billings));
SELECT setval(pg_get_serial_sequence('audit_logs', 'id'), (SELECT COALESCE(MAX(id), 1) FROM audit_logs));
SELECT setval(pg_get_serial_sequence('case_parties', 'id'), (SELECT COALESCE(MAX(id), 1) FROM case_parties));
SELECT setval(pg_get_serial_sequence('conflict_checks', 'id'), (SELECT COALESCE(MAX(id), 1) FROM conflict_checks));
SELECT setval(pg_get_serial_sequence('conflict_check_parties', 'id'), (SELECT COALESCE(MAX(id), 1) FROM conflict_check_parties));
