package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/ent/slapolicy"
	"github.com/bengobox/ticketing-service/internal/ent/ticketcategory"
	"github.com/bengobox/ticketing-service/internal/modules/sla"
)

// slugify converts a name to a URL-friendly slug.
func slugify(name string) string {
	s := strings.ToLower(name)
	re := regexp.MustCompile(`[^a-z0-9]+`)
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

type categorySpec struct {
	name      string
	icon      string
	sortOrder int
}

type slaSpec struct {
	priority             slapolicy.Priority
	name                 string
	firstResponseMinutes int
	resolutionMinutes    int
	sortOrder            int
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Prefer the direct PostgreSQL URL (POSTGRES_MIGRATE_URL) to bypass PgBouncer for seed DDL/DML.
	dbURL := os.Getenv("POSTGRES_MIGRATE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("POSTGRES_URL")
	}
	if dbURL == "" {
		log.Fatalln("POSTGRES_URL is required")
	}

	tenantIDStr := os.Getenv("TENANT_ID")
	if tenantIDStr == "" {
		log.Fatalln("TENANT_ID is required")
	}
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		log.Fatalf("invalid TENANT_ID %q: %v", tenantIDStr, err)
	}

	log.Printf("connecting to database for tenant %s", tenantID)

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	drv := entsql.OpenDB(dialect.Postgres, db)
	client := ent.NewClient(ent.Driver(drv))
	defer client.Close()

	// SEED_SCOPE=kb loads only knowledge-base articles (KB_SET), leaving the tenant's categories and
	// SLA policies alone. Otherwise categories and SLAs are seeded as before, plus articles when
	// KB_SET is given.
	scope := os.Getenv("SEED_SCOPE")
	kbSet := os.Getenv("KB_SET")
	if scope != "" && scope != "all" && scope != "kb" {
		log.Fatalf("SEED_SCOPE must be all or kb, got %q", scope)
	}
	if scope == "kb" && kbSet == "" {
		log.Fatalln("SEED_SCOPE=kb needs KB_SET")
	}
	if kbSet != "" {
		created, skipped, err := seedKB(ctx, client, tenantID, kbSet)
		if err != nil {
			log.Fatalf("failed to seed knowledge base: %v", err)
		}
		log.Printf("Seeded %d articles from KB_SET %q (%d skipped as already existing).", created, kbSet, skipped)
	}
	if scope == "kb" {
		return
	}

	categories := []categorySpec{
		{name: "General", icon: "help-circle", sortOrder: 0},
		{name: "Billing", icon: "credit-card", sortOrder: 1},
		{name: "Technical Support", icon: "tool", sortOrder: 2},
		{name: "Account & Access", icon: "key", sortOrder: 3},
		{name: "Feature Request", icon: "star", sortOrder: 4},
		{name: "Order & Delivery", icon: "package", sortOrder: 5},
	}

	slaPolicies := []slaSpec{
		{
			priority:             slapolicy.PriorityLow,
			name:                 "Low Priority SLA",
			firstResponseMinutes: 480,
			resolutionMinutes:    2880,
			sortOrder:            0,
		},
		{
			priority:             slapolicy.PriorityNormal,
			name:                 "Normal SLA",
			firstResponseMinutes: 240,
			resolutionMinutes:    1440,
			sortOrder:            1,
		},
		{
			priority:             slapolicy.PriorityHigh,
			name:                 "High Priority SLA",
			firstResponseMinutes: 60,
			resolutionMinutes:    480,
			sortOrder:            2,
		},
		{
			priority:             slapolicy.PriorityUrgent,
			name:                 "Urgent SLA",
			firstResponseMinutes: 30,
			resolutionMinutes:    240,
			sortOrder:            3,
		},
		{
			priority:             slapolicy.PriorityCritical,
			name:                 "Critical SLA",
			firstResponseMinutes: 15,
			resolutionMinutes:    60,
			sortOrder:            4,
		},
	}

	var catCreated, catSkipped int
	for _, cat := range categories {
		exists, err := client.TicketCategory.Query().
			Where(
				ticketcategory.TenantID(tenantID),
				ticketcategory.Name(cat.name),
			).
			Exist(ctx)
		if err != nil {
			log.Fatalf("failed to check category %q: %v", cat.name, err)
		}
		if exists {
			log.Printf("  category %q already exists, skipping", cat.name)
			catSkipped++
			continue
		}

		icon := cat.icon
		_, err = client.TicketCategory.Create().
			SetTenantID(tenantID).
			SetName(cat.name).
			SetSlug(slugify(cat.name)).
			SetNillableIcon(&icon).
			SetSortOrder(cat.sortOrder).
			SetIsActive(true).
			Save(ctx)
		if err != nil {
			log.Fatalf("failed to create category %q: %v", cat.name, err)
		}
		log.Printf("  created category %q", cat.name)
		catCreated++
	}

	var slaCreated, slaSkipped int
	for _, pol := range slaPolicies {
		exists, err := client.SLAPolicy.Query().
			Where(
				slapolicy.TenantID(tenantID),
				slapolicy.PriorityEQ(pol.priority),
			).
			Exist(ctx)
		if err != nil {
			log.Fatalf("failed to check SLA policy %q: %v", pol.name, err)
		}
		if exists {
			log.Printf("  SLA policy for priority %q already exists, skipping", pol.priority)
			slaSkipped++
			continue
		}

		_, err = client.SLAPolicy.Create().
			SetTenantID(tenantID).
			SetName(pol.name).
			SetPriority(pol.priority).
			SetFirstResponseMinutes(pol.firstResponseMinutes).
			SetResolutionMinutes(pol.resolutionMinutes).
			SetSortOrder(pol.sortOrder).
			SetIsActive(true).
			Save(ctx)
		if err != nil {
			log.Fatalf("failed to create SLA policy %q: %v", pol.name, err)
		}
		log.Printf("  created SLA policy %q (priority: %s)", pol.name, pol.priority)
		slaCreated++
	}

	// The tenant's first business calendar: Monday to Friday 08:00 to 17:00 in Africa/Nairobi with
	// this year's and next year's Kenyan public holidays. The seeded policies stay 24x7 until an
	// admin points them at it.
	calCreated, err := sla.NewService(sla.NewEntRepository(client), zap.NewNop()).EnsureDefaultCalendar(ctx, tenantID)
	if err != nil {
		log.Fatalf("failed to seed the business calendar: %v", err)
	}
	if calCreated {
		log.Printf("  created business calendar %q with Kenyan public holidays", sla.DefaultCalendarName)
	} else {
		log.Printf("  business calendar already exists, skipping")
	}

	totalSkipped := catSkipped + slaSkipped
	log.Printf("Seeded %d categories, %d SLA policies (%d skipped as already existing).",
		catCreated, slaCreated, totalSkipped)
}
