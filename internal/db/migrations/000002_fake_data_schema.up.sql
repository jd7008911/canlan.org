-- Seed data migration
-- This file contains fake/sample data and depends on the schema created
-- by 000001_init_schema.up.sql. Apply after that migration.

-- Tokens
INSERT INTO tokens (id, symbol, name, decimals, contract_address, price_usd, is_active)
VALUES
  (uuid_generate_v4(), 'CAFL', 'CangLanFu Token', 18, '0xCAfLanFu00000000000000000000000000000001', 1.000000000000000000, true),
  (uuid_generate_v4(), 'USDT', 'Tether USD', 6, '0xdAC17F958D2ee523a2206206994597C13D831ec7', 1.000000000000000000, true),
  (uuid_generate_v4(), 'ETH', 'Ether', 18, '0x0000000000000000000000000000000000000000', 1800.000000000000000000, true);

-- Create some users
INSERT INTO users (id, wallet_address, referral_code, node_level)
VALUES
  (uuid_generate_v4(), '0x1000000000000000000000000000000000000100', 'REF100', 1),
  (uuid_generate_v4(), '0x1000000000000000000000000000000000000101', 'REF101', 0),
  (uuid_generate_v4(), '0x1000000000000000000000000000000000000102', 'REF102', 0),
  (uuid_generate_v4(), '0x1000000000000000000000000000000000000103', 'REF103', 0),
  (uuid_generate_v4(), '0x1000000000000000000000000000000000000104', 'REF104', 2),
  (uuid_generate_v4(), '0x1000000000000000000000000000000000000105', 'REF105', 1);

-- Profiles
INSERT INTO user_profiles (user_id, nickname, avatar_url)
SELECT id, 'user_' || right(wallet_address, 4), NULL FROM users
WHERE wallet_address IN (
  '0x1000000000000000000000000000000000000100',
  '0x1000000000000000000000000000000000000101',
  '0x1000000000000000000000000000000000000102',
  '0x1000000000000000000000000000000000000103',
  '0x1000000000000000000000000000000000000104',
  '0x1000000000000000000000000000000000000105'
);

-- Set up simple referral relationships (invited_by)
UPDATE users SET invited_by = (SELECT id FROM users WHERE wallet_address = '0x1000000000000000000000000000000000000100')
WHERE wallet_address IN ('0x1000000000000000000000000000000000000101', '0x1000000000000000000000000000000000000102');

UPDATE users SET invited_by = (SELECT id FROM users WHERE wallet_address = '0x1000000000000000000000000000000000000101')
WHERE wallet_address = '0x1000000000000000000000000000000000000103';

-- User balances (some CAFL, some USDT)
INSERT INTO user_balances (user_id, token_id, balance)
SELECT u.id, t.id, b.amount FROM (
  VALUES
    ('0x1000000000000000000000000000000000000100', 10000.0),
    ('0x1000000000000000000000000000000000000101', 2500.5),
    ('0x1000000000000000000000000000000000000102', 120.75),
    ('0x1000000000000000000000000000000000000103', 0.0),
    ('0x1000000000000000000000000000000000000104', 50000.0),
    ('0x1000000000000000000000000000000000000105', 75.25)
  ) AS b(wallet, amount)
JOIN users u ON u.wallet_address = b.wallet
JOIN tokens t ON t.symbol = 'CAFL';

-- Some USDT balances for liquidity/testing
INSERT INTO user_balances (user_id, token_id, balance)
SELECT u.id, t.id, b.amount FROM (
  VALUES
    ('0x1000000000000000000000000000000000000100', 5000.0),
    ('0x1000000000000000000000000000000000000104', 12000.0)
  ) AS b(wallet, amount)
JOIN users u ON u.wallet_address = b.wallet
JOIN tokens t ON t.symbol = 'USDT';

-- Badges
INSERT INTO badges (id, name, description, icon_url, price_usd, benefits, tier)
VALUES
  (uuid_generate_v4(), 'Founders', 'Early backer badge', NULL, 50.0, '{"mining_boost":0.1}'::jsonb, 1),
  (uuid_generate_v4(), 'VIP', 'VIP member badge', NULL, 200.0, '{"combat_power_multiplier":1.5}'::jsonb, 2);

-- Assign a badge to a user
INSERT INTO user_badges (id, user_id, badge_id, is_active)
SELECT uuid_generate_v4(), u.id, b.id, true
FROM users u, badges b
WHERE u.wallet_address = '0x1000000000000000000000000000000000000104' AND b.name = 'VIP';

-- Liquidity pool and LP positions
INSERT INTO liquidity_pools (id, name, token0_id, token1_id, total_liquidity_usd, apr)
SELECT uuid_generate_v4(), 'CAFL-USDT', t0.id, t1.id, 100000.0, 12.5
FROM tokens t0, tokens t1 WHERE t0.symbol = 'CAFL' AND t1.symbol = 'USDT';

-- Add LP position for main user
INSERT INTO lp_positions (id, user_id, pool_id, lp_amount, share_percentage)
SELECT uuid_generate_v4(), u.id, p.id, 1000.0, 1.0
FROM users u, liquidity_pools p
WHERE u.wallet_address = '0x1000000000000000000000000000000000000100' AND p.name = 'CAFL-USDT';

-- LP add history
INSERT INTO lp_transactions (id, user_id, position_id, type, token0_amount, token1_amount, lp_amount)
SELECT uuid_generate_v4(), u.id, lp.id, 'add', 5000.0, 5000.0, 1000.0
FROM users u, lp_positions lp WHERE u.wallet_address = '0x1000000000000000000000000000000000000100' AND lp.user_id = u.id;

-- Mining machines and earnings
INSERT INTO mining_machines (id, user_id, level, static_rate, acceleration_rate, total_mined)
SELECT uuid_generate_v4(), u.id, 2, 0.05, 0.005, 10.0
FROM users u WHERE u.wallet_address = '0x1000000000000000000000000000000000000104';

INSERT INTO mining_earnings (id, user_id, machine_id, amount, earning_type, date)
SELECT uuid_generate_v4(), m.user_id, m.id, 0.05, 'static', CURRENT_DATE - INTERVAL '1 day'
FROM mining_machines m WHERE m.user_id = (SELECT id FROM users WHERE wallet_address = '0x1000000000000000000000000000000000000104');

-- Burns
INSERT INTO burns (id, user_id, token_id, amount, combat_power_gained)
SELECT uuid_generate_v4(), u.id, t.id, 100.0, 100.0
FROM users u, tokens t WHERE u.wallet_address = '0x1000000000000000000000000000000000000102' AND t.symbol = 'CAFL';

-- Combat power snapshot
INSERT INTO combat_power (user_id, personal_power, network_power, lp_weight, burn_power)
SELECT u.id, 120.0, 50.0, 10.0, 100.0 FROM users u WHERE u.wallet_address = '0x1000000000000000000000000000000000000102';

-- Mint a couple of blocks and distribute simple rewards
INSERT INTO mint_blocks (id, block_number, total_reward)
VALUES (uuid_generate_v4(), 100000, 500.0), (uuid_generate_v4(), 100001, 600.0);

-- Block rewards to a user
INSERT INTO block_rewards (id, user_id, block_id, amount, weight_type, weight_value)
SELECT uuid_generate_v4(), u.id, b.id, 25.0, 'transaction', 1.0
FROM users u, mint_blocks b
WHERE u.wallet_address = '0x1000000000000000000000000000000000000100'
LIMIT 1;

-- Simple governance proposal and vote
INSERT INTO governance_proposals (id, proposer_id, title, description, proposal_type, voting_end)
SELECT uuid_generate_v4(), u.id, 'Reduce fees', 'Proposal to reduce swap fees by 0.1%', 'parameter', CURRENT_TIMESTAMP + INTERVAL '7 days'
FROM users u WHERE u.wallet_address = '0x1000000000000000000000000000000000000104';

INSERT INTO votes (id, user_id, proposal_id, vote_power, vote_choice)
SELECT uuid_generate_v4(), u.id, p.id, 100.0, 'for'
FROM users u, governance_proposals p
WHERE u.wallet_address IN ('0x1000000000000000000000000000000000000100', '0x1000000000000000000000000000000000000104')
LIMIT 2;

-- Referral earnings example
INSERT INTO referral_earnings (id, user_id, from_user_id, amount, token_id, earning_type)
SELECT uuid_generate_v4(), u.id, fu.id, 10.0, t.id, 'purchase'
FROM users u, users fu, tokens t
WHERE u.wallet_address = '0x1000000000000000000000000000000000000100' AND fu.wallet_address = '0x1000000000000000000000000000000000000101' AND t.symbol = 'CAFL';

-- End of seed data
