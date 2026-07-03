-- +goose Up
INSERT INTO role_permissions (id, role_id, permission_id, created_at)
SELECT 'rperm_standard_knowledge_read_v2', r.id, p.id, now()
FROM auth_roles r, auth_permissions p
WHERE r.code = 'standard' AND p.code = 'knowledge:read'
  AND NOT EXISTS (
    SELECT 1 FROM role_permissions rp2
    WHERE rp2.role_id = r.id AND rp2.permission_id = p.id
  );

-- +goose Down
DELETE FROM role_permissions
WHERE id = 'rperm_standard_knowledge_read_v2';
