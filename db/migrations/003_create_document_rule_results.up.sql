-- Drop the table if it exists to ensure clean slate
DROP TABLE IF EXISTS document_rule_results CASCADE;

-- Create document_rule_results table with foreign key constraints
CREATE TABLE IF NOT EXISTS document_rule_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID REFERENCES documents(id) ON DELETE CASCADE,
    rule_id UUID REFERENCES compliance_rules(id) ON DELETE CASCADE,
    status VARCHAR(20),
    details JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for performance and foreign key relationships
CREATE INDEX IF NOT EXISTS idx_document_rule_results_document_id ON document_rule_results(document_id);
CREATE INDEX IF NOT EXISTS idx_document_rule_results_rule_id ON document_rule_results(rule_id);
CREATE INDEX IF NOT EXISTS idx_document_rule_results_status ON document_rule_results(status);