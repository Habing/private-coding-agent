INSERT INTO users (tenant_id, email, password_hash, name, role)
VALUES (
  (SELECT id FROM tenants WHERE slug = 'default'),
  'demo@example.com',
  '$2a$10$WJBaC0mXl/yIgPXKW8WbPujOAidLdmaDPlduPdV8i11ZHaFvcgUrC',
  'Demo',
  'admin'
)
ON CONFLICT (tenant_id, email) DO UPDATE SET
  password_hash = EXCLUDED.password_hash,
  name = EXCLUDED.name,
  role = EXCLUDED.role;
