-- Drop the table if it exists to ensure clean slate
DROP TABLE IF EXISTS compliance_rules CASCADE;

-- Create compliance_rules table
CREATE TABLE IF NOT EXISTS compliance_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    description TEXT,
    pattern TEXT,
    severity VARCHAR(20),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create an index on frequently queried columns
CREATE INDEX IF NOT EXISTS idx_compliance_rules_severity ON compliance_rules(severity);
CREATE INDEX IF NOT EXISTS idx_compliance_rules_name ON compliance_rules(name);