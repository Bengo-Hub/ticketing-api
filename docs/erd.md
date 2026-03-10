# Ticketing Service – Entity Relationship Diagram

The Ticketing service manages customer support tickets, SLA tracking, knowledge base, and customer satisfaction. Ent schemas model the domain and power migrations.

> **Conventions**
> - UUID primary keys (`id UUID PRIMARY KEY DEFAULT gen_random_uuid()`).
> - `tenant_id UUID NOT NULL` on all operational tables for multi-tenant isolation.
> - Timestamps are `TIMESTAMPTZ` with timezone awareness.
> - All tables include `created_at TIMESTAMPTZ DEFAULT NOW()` and `updated_at TIMESTAMPTZ DEFAULT NOW()`.
> - Soft deletes via `deleted_at TIMESTAMPTZ` where applicable.
> - Vector columns use `vector(1536)` for AI-powered semantic search (pgvector extension).

---

## Database Extensions

- `pgvector` - Vector similarity search for AI-powered queries
- `uuid-ossp` - UUID generation
- `pg_trgm` - Trigram matching for fuzzy search
- `btree_gin` - GIN indexes for composite queries

---

## Identity & Access Management

### teams

**Purpose**: Support team groupings.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | Team identifier |
| `tenant_id` | UUID | NOT NULL, FK → tenants | Tenant isolation |
| `name` | VARCHAR(255) | NOT NULL | Team name |
| `description` | TEXT | | Team description |
| `is_active` | BOOLEAN | DEFAULT true | Active status |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | Creation timestamp |
| `updated_at` | TIMESTAMPTZ | DEFAULT NOW() | Last update timestamp |

**Indexes**:
- `idx_teams_tenant_id` ON `tenant_id`
- `idx_teams_name` ON `name`
- `idx_teams_is_active` ON `is_active`

**Relations**:
- `tenant_id` → `tenants(id)` (via auth-service sync)

### agents

**Purpose**: Agent profiles (references auth-service users).

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | Agent identifier |
| `tenant_id` | UUID | NOT NULL, FK → tenants | Tenant isolation |
| `user_id` | UUID | NOT NULL, FK → users | User reference (from auth-service) |
| `team_id` | UUID | FK → teams(id) | Team assignment |
| `status` | VARCHAR(20) | CHECK | active, inactive, away, busy |
| `max_concurrent_tickets` | INTEGER | DEFAULT 5 | Max concurrent tickets |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | Creation timestamp |
| `updated_at` | TIMESTAMPTZ | DEFAULT NOW() | Last update timestamp |

**Indexes**:
- `idx_agents_tenant_id` ON `tenant_id`
- `idx_agents_user_id` ON `user_id`
- `idx_agents_team_id` ON `team_id`
- `idx_agents_status` ON `status`

**Relations**:
- `tenant_id` → `tenants(id)` (via auth-service sync)
- `user_id` → `users(id)` (references auth-service)
- `team_id` → `teams(id)`

### agent_availability

**Purpose**: Agent availability schedules.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | Availability record identifier |
| `agent_id` | UUID | NOT NULL, FK → agents(id) | Agent reference |
| `day_of_week` | INTEGER | CHECK (0-6) | Day of week (0=Sunday) |
| `start_time` | TIME | | Start time |
| `end_time` | TIME | | End time |
| `is_available` | BOOLEAN | DEFAULT true | Available flag |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | Creation timestamp |
| `updated_at` | TIMESTAMPTZ | DEFAULT NOW() | Last update timestamp |

**Indexes**:
- `idx_agent_availability_agent` ON `agent_id`
- `idx_agent_availability_day` ON `day_of_week`

**Relations**:
- `agent_id` → `agents(id)`

---

## Ticket Management

### ticket_categories

**Purpose**: Ticket category definitions.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | Category identifier |
| `tenant_id` | UUID | NOT NULL, FK → tenants | Tenant isolation |
| `name` | VARCHAR(255) | NOT NULL | Category name |
| `description` | TEXT | | Category description |
| `is_active` | BOOLEAN | DEFAULT true | Active status |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | Creation timestamp |
| `updated_at` | TIMESTAMPTZ | DEFAULT NOW() | Last update timestamp |

**Indexes**:
- `idx_ticket_categories_tenant_id` ON `tenant_id`
- `idx_ticket_categories_name` ON `name`

**Relations**:
- `tenant_id` → `tenants(id)` (via auth-service sync)

### ticket_priorities

**Purpose**: Ticket priority definitions.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | Priority identifier |
| `tenant_id` | UUID | NOT NULL, FK → tenants | Tenant isolation |
| `name` | VARCHAR(50) | NOT NULL | Priority name (low, medium, high, urgent, critical) |
| `level` | INTEGER | NOT NULL | Priority level (1-5) |
| `color` | VARCHAR(7) | | Color code |
| `is_active` | BOOLEAN | DEFAULT true | Active status |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | Creation timestamp |
| `updated_at` | TIMESTAMPTZ | DEFAULT NOW() | Last update timestamp |

**Indexes**:
- `idx_ticket_priorities_tenant_id` ON `tenant_id`
- `idx_ticket_priorities_level` ON `level`

**Relations**:
- `tenant_id` → `tenants(id)` (via auth-service sync)

### tickets

**Purpose**: Canonical ticket records.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | Ticket identifier |
| `tenant_id` | UUID | NOT NULL, FK → tenants | Tenant isolation |
| `ticket_number` | VARCHAR(50) | NOT NULL, UNIQUE(tenant_id, ticket_number) | Ticket number (e.g., TKT-001) |
| `subject` | VARCHAR(500) | NOT NULL | Ticket subject |
| `description` | TEXT | NOT NULL | Ticket description |
| `category_id` | UUID | FK → ticket_categories(id) | Category reference |
| `priority_id` | UUID | FK → ticket_priorities(id) | Priority reference |
| `status` | VARCHAR(20) | CHECK | new, open, in_progress, resolved, closed, cancelled |
| `source` | VARCHAR(50) | | Source (web, email, api, chat, phone) |
| `customer_id` | UUID | FK → users(id) | Customer reference (from auth-service) |
| `assigned_agent_id` | UUID | FK → agents(id) | Assigned agent |
| `assigned_team_id` | UUID | FK → teams(id) | Assigned team |
| `project_id` | UUID | | Project reference (from projects-service) |
| `order_id` | UUID | | Order reference (from ordering/POS services) |
| `invoice_id` | UUID | | Invoice reference (from treasury-api) |
| `metadata` | JSONB | | Additional metadata |
| `embedding` | vector(1536) | | Vector embedding for semantic search |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | Creation timestamp |
| `updated_at` | TIMESTAMPTZ | DEFAULT NOW() | Last update timestamp |
| `resolved_at` | TIMESTAMPTZ | | Resolution timestamp |
| `closed_at` | TIMESTAMPTZ | | Closure timestamp |

**Indexes**:
- `idx_tickets_tenant_id` ON `tenant_id`
- `idx_tickets_ticket_number` ON `ticket_number`
- `idx_tickets_status` ON `status`
- `idx_tickets_category` ON `category_id`
- `idx_tickets_priority` ON `priority_id`
- `idx_tickets_customer` ON `customer_id`
- `idx_tickets_assigned_agent` ON `assigned_agent_id`
- `idx_tickets_assigned_team` ON `assigned_team_id`
- `idx_tickets_created_at` ON `created_at`
- `idx_tickets_embedding` ON `embedding` USING ivfflat (vector_cosine_ops)

**Relations**:
- `tenant_id` → `tenants(id)` (via auth-service sync)
- `category_id` → `ticket_categories(id)`
- `priority_id` → `ticket_priorities(id)`
- `customer_id` → `users(id)` (references auth-service)
- `assigned_agent_id` → `agents(id)`
- `assigned_team_id` → `teams(id)`

### ticket_tags

**Purpose**: Ticket tag definitions.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | Tag identifier |
| `tenant_id` | UUID | NOT NULL, FK → tenants | Tenant isolation |
| `name` | VARCHAR(100) | NOT NULL | Tag name |
| `color` | VARCHAR(7) | | Color code |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | Creation timestamp |

**Indexes**:
- `idx_ticket_tags_tenant_id` ON `tenant_id`
- `idx_ticket_tags_name` ON `name`

**Relations**:
- `tenant_id` → `tenants(id)` (via auth-service sync)

### ticket_tag_assignments

**Purpose**: Ticket-tag relationships.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | Assignment identifier |
| `ticket_id` | UUID | NOT NULL, FK → tickets(id) | Ticket reference |
| `tag_id` | UUID | NOT NULL, FK → ticket_tags(id) | Tag reference |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | Creation timestamp |

**Indexes**:
- `idx_ticket_tag_assignments_ticket` ON `ticket_id`
- `idx_ticket_tag_assignments_tag` ON `tag_id`
- UNIQUE(`ticket_id`, `tag_id`)

**Relations**:
- `ticket_id` → `tickets(id)`
- `tag_id` → `ticket_tags(id)`

---

## Ticket Communication

### ticket_comments

**Purpose**: Ticket comments and replies.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | Comment identifier |
| `ticket_id` | UUID | NOT NULL, FK → tickets(id) | Ticket reference |
| `author_id` | UUID | FK → users(id) | Author reference (from auth-service) |
| `comment_type` | VARCHAR(20) | CHECK | public, internal, system |
| `content` | TEXT | NOT NULL | Comment content |
| `is_internal` | BOOLEAN | DEFAULT false | Internal note flag |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | Creation timestamp |
| `updated_at` | TIMESTAMPTZ | DEFAULT NOW() | Last update timestamp |

**Indexes**:
- `idx_ticket_comments_ticket` ON `ticket_id`
- `idx_ticket_comments_author` ON `author_id`
- `idx_ticket_comments_created_at` ON `created_at`

**Relations**:
- `ticket_id` → `tickets(id)`
- `author_id` → `users(id)` (references auth-service)

### ticket_attachments

**Purpose**: File attachments for tickets.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | Attachment identifier |
| `ticket_id` | UUID | NOT NULL, FK → tickets(id) | Ticket reference |
| `comment_id` | UUID | FK → ticket_comments(id) | Comment reference (optional) |
| `file_name` | VARCHAR(255) | NOT NULL | File name |
| `file_size` | BIGINT | | File size (bytes) |
| `file_type` | VARCHAR(100) | | MIME type |
| `storage_url` | VARCHAR(500) | NOT NULL | Storage URL (S3) |
| `uploaded_by_id` | UUID | FK → users(id) | Uploader reference (from auth-service) |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | Creation timestamp |

**Indexes**:
- `idx_ticket_attachments_ticket` ON `ticket_id`
- `idx_ticket_attachments_comment` ON `comment_id`

**Relations**:
- `ticket_id` → `tickets(id)`
- `comment_id` → `ticket_comments(id)`
- `uploaded_by_id` → `users(id)` (references auth-service)

---

## Assignment & Routing

### ticket_assignments

**Purpose**: Ticket assignment records.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | Assignment identifier |
| `ticket_id` | UUID | NOT NULL, FK → tickets(id) | Ticket reference |
| `agent_id` | UUID | FK → agents(id) | Agent reference |
| `team_id` | UUID | FK → teams(id) | Team reference |
| `assigned_by_id` | UUID | FK → users(id) | Assigner reference (from auth-service) |
| `assignment_type` | VARCHAR(20) | CHECK | automatic, manual, escalation |
| `assigned_at` | TIMESTAMPTZ | DEFAULT NOW() | Assignment timestamp |
| `unassigned_at` | TIMESTAMPTZ | | Unassignment timestamp |

**Indexes**:
- `idx_ticket_assignments_ticket` ON `ticket_id`
- `idx_ticket_assignments_agent` ON `agent_id`
- `idx_ticket_assignments_team` ON `team_id`
- `idx_ticket_assignments_assigned_at` ON `assigned_at`

**Relations**:
- `ticket_id` → `tickets(id)`
- `agent_id` → `agents(id)`
- `team_id` → `teams(id)`
- `assigned_by_id` → `users(id)` (references auth-service)

### assignment_rules

**Purpose**: Automatic assignment rule definitions.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | Rule identifier |
| `tenant_id` | UUID | NOT NULL, FK → tenants | Tenant isolation |
| `name` | VARCHAR(255) | NOT NULL | Rule name |
| `rule_type` | VARCHAR(50) | CHECK | round_robin, skill_based, load_based |
| `conditions_json` | JSONB | | Rule conditions |
| `is_active` | BOOLEAN | DEFAULT true | Active flag |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | Creation timestamp |
| `updated_at` | TIMESTAMPTZ | DEFAULT NOW() | Last update timestamp |

**Indexes**:
- `idx_assignment_rules_tenant_id` ON `tenant_id`
- `idx_assignment_rules_rule_type` ON `rule_type`

**Relations**:
- `tenant_id` → `tenants(id)` (via auth-service sync)

---

## SLA Management

### sla_policies

**Purpose**: SLA policy definitions.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | SLA policy identifier |
| `tenant_id` | UUID | NOT NULL, FK → tenants | Tenant isolation |
| `name` | VARCHAR(255) | NOT NULL | Policy name |
| `response_time_minutes` | INTEGER | | Response time (minutes) |
| `resolution_time_hours` | INTEGER | | Resolution time (hours) |
| `priority_id` | UUID | FK → ticket_priorities(id) | Priority reference |
| `category_id` | UUID | FK → ticket_categories(id) | Category reference |
| `is_active` | BOOLEAN | DEFAULT true | Active flag |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | Creation timestamp |
| `updated_at` | TIMESTAMPTZ | DEFAULT NOW() | Last update timestamp |

**Indexes**:
- `idx_sla_policies_tenant_id` ON `tenant_id`
- `idx_sla_policies_priority` ON `priority_id`
- `idx_sla_policies_category` ON `category_id`

**Relations**:
- `tenant_id` → `tenants(id)` (via auth-service sync)
- `priority_id` → `ticket_priorities(id)`
- `category_id` → `ticket_categories(id)`

### sla_records

**Purpose**: SLA tracking per ticket.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | SLA record identifier |
| `ticket_id` | UUID | NOT NULL, FK → tickets(id) | Ticket reference |
| `sla_policy_id` | UUID | NOT NULL, FK → sla_policies(id) | SLA policy reference |
| `response_deadline` | TIMESTAMPTZ | | Response deadline |
| `resolution_deadline` | TIMESTAMPTZ | | Resolution deadline |
| `first_response_at` | TIMESTAMPTZ | | First response timestamp |
| `resolved_at` | TIMESTAMPTZ | | Resolution timestamp |
| `response_breached` | BOOLEAN | DEFAULT false | Response breach flag |
| `resolution_breached` | BOOLEAN | DEFAULT false | Resolution breach flag |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | Creation timestamp |
| `updated_at` | TIMESTAMPTZ | DEFAULT NOW() | Last update timestamp |

**Indexes**:
- `idx_sla_records_ticket` ON `ticket_id`
- `idx_sla_records_sla_policy` ON `sla_policy_id`
- `idx_sla_records_response_deadline` ON `response_deadline`
- `idx_sla_records_resolution_deadline` ON `resolution_deadline`

**Relations**:
- `ticket_id` → `tickets(id)`
- `sla_policy_id` → `sla_policies(id)`

---

## Knowledge Base

### kb_categories

**Purpose**: Knowledge base category structure.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | Category identifier |
| `tenant_id` | UUID | NOT NULL, FK → tenants | Tenant isolation |
| `name` | VARCHAR(255) | NOT NULL | Category name |
| `description` | TEXT | | Category description |
| `parent_id` | UUID | FK → kb_categories(id) | Parent category |
| `is_active` | BOOLEAN | DEFAULT true | Active flag |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | Creation timestamp |
| `updated_at` | TIMESTAMPTZ | DEFAULT NOW() | Last update timestamp |

**Indexes**:
- `idx_kb_categories_tenant_id` ON `tenant_id`
- `idx_kb_categories_parent_id` ON `parent_id`

**Relations**:
- `tenant_id` → `tenants(id)` (via auth-service sync)
- `parent_id` → `kb_categories(id)` (self-referential)

### kb_articles

**Purpose**: Knowledge base articles.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | Article identifier |
| `tenant_id` | UUID | NOT NULL, FK → tenants | Tenant isolation |
| `category_id` | UUID | FK → kb_categories(id) | Category reference |
| `title` | VARCHAR(500) | NOT NULL | Article title |
| `content` | TEXT | NOT NULL | Article content |
| `status` | VARCHAR(20) | CHECK | draft, published, archived |
| `view_count` | INTEGER | DEFAULT 0 | View count |
| `helpful_count` | INTEGER | DEFAULT 0 | Helpful count |
| `not_helpful_count` | INTEGER | DEFAULT 0 | Not helpful count |
| `embedding` | vector(1536) | | Vector embedding for semantic search |
| `created_by_id` | UUID | FK → users(id) | Creator reference (from auth-service) |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | Creation timestamp |
| `updated_at` | TIMESTAMPTZ | DEFAULT NOW() | Last update timestamp |

**Indexes**:
- `idx_kb_articles_tenant_id` ON `tenant_id`
- `idx_kb_articles_category` ON `category_id`
- `idx_kb_articles_status` ON `status`
- `idx_kb_articles_embedding` ON `embedding` USING ivfflat (vector_cosine_ops)

**Relations**:
- `tenant_id` → `tenants(id)` (via auth-service sync)
- `category_id` → `kb_categories(id)`
- `created_by_id` → `users(id)` (references auth-service)

---

## Customer Satisfaction

### satisfaction_surveys

**Purpose**: Satisfaction survey definitions.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | Survey identifier |
| `tenant_id` | UUID | NOT NULL, FK → tenants | Tenant isolation |
| `name` | VARCHAR(255) | NOT NULL | Survey name |
| `survey_type` | VARCHAR(20) | CHECK | csat, nps, custom |
| `is_active` | BOOLEAN | DEFAULT true | Active flag |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | Creation timestamp |
| `updated_at` | TIMESTAMPTZ | DEFAULT NOW() | Last update timestamp |

**Indexes**:
- `idx_satisfaction_surveys_tenant_id` ON `tenant_id`
- `idx_satisfaction_surveys_survey_type` ON `survey_type`

**Relations**:
- `tenant_id` → `tenants(id)` (via auth-service sync)

### satisfaction_responses

**Purpose**: Survey responses.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | Response identifier |
| `ticket_id` | UUID | FK → tickets(id) | Ticket reference |
| `survey_id` | UUID | NOT NULL, FK → satisfaction_surveys(id) | Survey reference |
| `customer_id` | UUID | FK → users(id) | Customer reference (from auth-service) |
| `score` | INTEGER | | Score (1-5 for CSAT, 0-10 for NPS) |
| `comment` | TEXT | | Additional comment |
| `responded_at` | TIMESTAMPTZ | DEFAULT NOW() | Response timestamp |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | Creation timestamp |

**Indexes**:
- `idx_satisfaction_responses_ticket` ON `ticket_id`
- `idx_satisfaction_responses_survey` ON `survey_id`
- `idx_satisfaction_responses_customer` ON `customer_id`

**Relations**:
- `ticket_id` → `tickets(id)`
- `survey_id` → `satisfaction_surveys(id)`
- `customer_id` → `users(id)` (references auth-service)

---

## Integration Points

### External Service References

**Auth Service**:
- `agents.user_id` → `users(id)` (user identity)
- `tickets.customer_id` → `users(id)` (customer identity)
- `ticket_comments.author_id` → `users(id)` (comment author)

**Projects Service**:
- `tickets.project_id` → References project UUID (from projects-service)

**Treasury Service**:
- `tickets.invoice_id` → References invoice UUID (from treasury-api)

**Ordering/POS Services**:
- `tickets.order_id` → References order UUID (from ordering/POS services)

---

## Views & Functions

### Recommended Views

**v_ticket_summary**:
- Ticket details with assignments, comments, and SLA status

**v_agent_performance**:
- Agent metrics aggregated (tickets handled, response time, resolution time)

**v_sla_compliance**:
- SLA compliance metrics by policy, priority, category

**v_customer_satisfaction**:
- Satisfaction scores aggregated by period, category, agent

---

## Maintenance Notes

- Maintain this document alongside Ent schema changes.
- After changing Ent schemas, run `go generate ./internal/ent` and refresh the ERD.
- Vector embeddings are generated using OpenAI embeddings API or similar (1536 dimensions).
- User management is handled by auth-service - reference user IDs only.

