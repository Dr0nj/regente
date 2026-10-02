package db

func schemaV31() string {
	return `ALTER TABLE agent_tokens ADD COLUMN certificate_sha256 TEXT NOT NULL DEFAULT '';`
}
