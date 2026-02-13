-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Users & Wallets
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    wallet_address VARCHAR(42) UNIQUE NOT NULL,
    parent_id UUID REFERENCES users(id),
    referral_code VARCHAR(20) UNIQUE NOT NULL,
    invited_by UUID REFERENCES users(id),
    node_level INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE user_profiles (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    nickname VARCHAR(100),
    avatar_url TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Tokens
CREATE TABLE tokens (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    symbol VARCHAR(20) UNIQUE NOT NULL,
    name VARCHAR(100) NOT NULL,
    decimals INT DEFAULT 18,
    contract_address VARCHAR(42),
    price_usd DECIMAL(36,18) DEFAULT 0,
    price_updated_at TIMESTAMP,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- User token balances
CREATE TABLE user_balances (
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    token_id UUID REFERENCES tokens(id),
    balance DECIMAL(36,18) DEFAULT 0,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, token_id)
);

-- Badges
CREATE TABLE badges (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) NOT NULL,
    description TEXT,
    icon_url TEXT,
    price_usd DECIMAL(36,18) NOT NULL,
    benefits JSONB, -- e.g., {"mining_boost": 0.1, "combat_power_multiplier": 1.5}
    tier INT DEFAULT 1,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- User badges
CREATE TABLE user_badges (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    badge_id UUID REFERENCES badges(id),
    purchased_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expiry_date TIMESTAMP, -- null if permanent
    is_active BOOLEAN DEFAULT true,
    UNIQUE(user_id, badge_id)
);

-- Purchases (token subscriptions)
CREATE TABLE purchases (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    token_id UUID REFERENCES tokens(id),
    amount DECIMAL(36,18) NOT NULL,
    price_usd DECIMAL(36,18) NOT NULL,
    total_value DECIMAL(36,18) NOT NULL,
    payment_token_id UUID REFERENCES tokens(id),
    status VARCHAR(20) DEFAULT 'pending', -- pending, completed, failed, cancelled
    tx_hash VARCHAR(66),
    completed_at TIMESTAMP,
    expiry_date TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Swaps
CREATE TABLE swaps (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    from_token_id UUID REFERENCES tokens(id),
    to_token_id UUID REFERENCES tokens(id),
    from_amount DECIMAL(36,18) NOT NULL,
    to_amount DECIMAL(36,18) NOT NULL,
    rate DECIMAL(36,18) NOT NULL,
    tx_hash VARCHAR(66),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Liquidity Pools
CREATE TABLE liquidity_pools (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) NOT NULL,
    token0_id UUID REFERENCES tokens(id),
    token1_id UUID REFERENCES tokens(id),
    total_liquidity_usd DECIMAL(36,18) DEFAULT 0,
    apr DECIMAL(10,4) DEFAULT 0,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- User LP positions
CREATE TABLE lp_positions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    pool_id UUID REFERENCES liquidity_pools(id),
    lp_amount DECIMAL(36,18) DEFAULT 0,
    share_percentage DECIMAL(10,6) DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- LP add/remove history
CREATE TABLE lp_transactions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id),
    position_id UUID REFERENCES lp_positions(id),
    type VARCHAR(10), -- 'add', 'remove'
    token0_amount DECIMAL(36,18),
    token1_amount DECIMAL(36,18),
    lp_amount DECIMAL(36,18),
    tx_hash VARCHAR(66),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Combat Power
CREATE TABLE combat_power (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    personal_power DECIMAL(36,18) DEFAULT 0,
    network_power DECIMAL(36,18) DEFAULT 0,
    lp_weight DECIMAL(36,18) DEFAULT 0,
    burn_power DECIMAL(36,18) DEFAULT 0,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Combat Power History (daily snapshots)
CREATE TABLE combat_power_history (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id),
    personal_power DECIMAL(36,18),
    date DATE NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Burns
CREATE TABLE burns (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    token_id UUID REFERENCES tokens(id),
    amount DECIMAL(36,18) NOT NULL,
    combat_power_gained DECIMAL(36,18),
    tx_hash VARCHAR(66),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Mining Machines
CREATE TABLE mining_machines (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    level INT DEFAULT 1,
    static_rate DECIMAL(36,18) DEFAULT 0.01, -- tokens per day
    acceleration_rate DECIMAL(36,18) DEFAULT 0.001,
    total_mined DECIMAL(36,18) DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Mining Earnings
CREATE TABLE mining_earnings (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id),
    machine_id UUID REFERENCES mining_machines(id),
    amount DECIMAL(36,18) NOT NULL,
    earning_type VARCHAR(20), -- 'static', 'acceleration'
    date DATE NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Block Rewards (Mint Blocks)
CREATE TABLE mint_blocks (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    block_number BIGINT UNIQUE NOT NULL,
    total_reward DECIMAL(36,18) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    distributed BOOLEAN DEFAULT false,
    distributed_at TIMESTAMP
);

-- User Block Rewards
CREATE TABLE block_rewards (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id),
    block_id UUID REFERENCES mint_blocks(id),
    amount DECIMAL(36,18) NOT NULL,
    weight_type VARCHAR(20), -- 'transaction', 'lp', 'burn'
    weight_value DECIMAL(36,18),
    claimed BOOLEAN DEFAULT false,
    claimed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Weight snapshots for block rewards
CREATE TABLE weight_snapshots (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id),
    block_id UUID REFERENCES mint_blocks(id),
    transaction_weight DECIMAL(36,18) DEFAULT 0,
    lp_weight DECIMAL(36,18) DEFAULT 0,
    burn_weight DECIMAL(36,18) DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Node Levels
CREATE TABLE node_levels (
    level INT PRIMARY KEY,
    required_team_power DECIMAL(36,18) NOT NULL,
    required_direct_members INT NOT NULL,
    rights JSONB,
    gift_limit DECIMAL(36,18) DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- User Node Progression
CREATE TABLE user_nodes (
    user_id UUID PRIMARY KEY REFERENCES users(id),
    current_level INT DEFAULT 0,
    team_power DECIMAL(36,18) DEFAULT 0,
    team_members INT DEFAULT 0,
    direct_referrals INT DEFAULT 0,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Withdrawals
CREATE TABLE withdrawals (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id),
    token_id UUID REFERENCES tokens(id),
    amount DECIMAL(36,18) NOT NULL,
    status VARCHAR(20) DEFAULT 'pending', -- pending, completed, failed
    tx_hash VARCHAR(66),
    completed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Withdrawal Limits (daily/monthly)
CREATE TABLE withdrawal_limits (
    user_id UUID PRIMARY KEY REFERENCES users(id),
    daily_limit DECIMAL(36,18) DEFAULT 1000,
    monthly_limit DECIMAL(36,18) DEFAULT 30000,
    daily_used DECIMAL(36,18) DEFAULT 0,
    monthly_used DECIMAL(36,18) DEFAULT 0,
    last_daily_reset DATE DEFAULT CURRENT_DATE,
    last_monthly_reset DATE DEFAULT CURRENT_DATE,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Governance Proposals
CREATE TABLE governance_proposals (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    proposer_id UUID REFERENCES users(id),
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    proposal_type VARCHAR(50), -- 'fee', 'parameter', 'upgrade', etc.
    status VARCHAR(20) DEFAULT 'active',
    voting_start TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    voting_end TIMESTAMP NOT NULL,
    for_votes DECIMAL(36,18) DEFAULT 0,
    against_votes DECIMAL(36,18) DEFAULT 0,
    abstain_votes DECIMAL(36,18) DEFAULT 0,
    quorum DECIMAL(5,2) DEFAULT 50,
    threshold DECIMAL(5,2) DEFAULT 50,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- User Votes
CREATE TABLE votes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id),
    proposal_id UUID REFERENCES governance_proposals(id),
    vote_power DECIMAL(36,18) NOT NULL,
    vote_choice VARCHAR(10), -- 'for', 'against', 'abstain'
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, proposal_id)
);

-- Referral Earnings
CREATE TABLE referral_earnings (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id),
    from_user_id UUID REFERENCES users(id),
    amount DECIMAL(36,18) NOT NULL,
    token_id UUID REFERENCES tokens(id),
    earning_type VARCHAR(20), -- 'purchase', 'mining', etc.
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_users_wallet ON users(wallet_address);
CREATE INDEX idx_users_referral_code ON users(referral_code);
CREATE INDEX idx_purchases_user_status ON purchases(user_id, status);
CREATE INDEX idx_swaps_user ON swaps(user_id);
CREATE INDEX idx_burns_user ON burns(user_id);
CREATE INDEX idx_lp_positions_user ON lp_positions(user_id);
CREATE INDEX idx_mining_earnings_user_date ON mining_earnings(user_id, date);
CREATE INDEX idx_block_rewards_user_claimed ON block_rewards(user_id, claimed);
CREATE INDEX idx_withdrawals_user_status ON withdrawals(user_id, status);