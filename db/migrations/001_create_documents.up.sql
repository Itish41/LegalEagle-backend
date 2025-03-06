-- Drop the table if it exists to ensure clean slate
DROP TABLE IF EXISTS documents CASCADE;

-- Create documents table with UUID primary key and appropriate constraints
CREATE TABLE IF NOT EXISTS documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT,
    file_type VARCHAR(50),
    original_url TEXT,
    ocr_text TEXT,
    parsed_data JSONB,
    risk_score FLOAT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create an index on frequently queried columns
CREATE INDEX IF NOT EXISTS idx_documents_file_type ON documents(file_type);
CREATE INDEX IF NOT EXISTS idx_documents_risk_score ON documents(risk_score);