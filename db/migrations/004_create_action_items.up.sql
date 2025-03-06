-- Drop the table if it exists to ensure clean slate
DROP TABLE IF EXISTS action_items CASCADE;

-- Create action_items table with foreign key constraints
CREATE TABLE IF NOT EXISTS action_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID REFERENCES documents(id) ON DELETE CASCADE,
    rule_id UUID REFERENCES compliance_rules(id) ON DELETE CASCADE,
    description TEXT NOT NULL,
    assigned_to VARCHAR(20),
    status VARCHAR(20),
    priority VARCHAR(20),
    due_date TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for performance and foreign key relationships
CREATE INDEX IF NOT EXISTS idx_action_items_document_id ON action_items(document_id);
CREATE INDEX IF NOT EXISTS idx_action_items_rule_id ON action_items(rule_id);
CREATE INDEX IF NOT EXISTS idx_action_items_status ON action_items(status);
CREATE INDEX IF NOT EXISTS idx_action_items_priority ON action_items(priority);