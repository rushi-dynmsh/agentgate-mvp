// Package relations wraps SpiceDB, the relationship graph. Where Cedar
// answers role-shaped questions ("may readers call read tools?"), SpiceDB
// answers relationship-shaped ones ("does alice OWN record 2?") — questions
// about specific objects that flat roles can't express.
//
// The schema is deliberately tiny: users own records, and owning a record is
// what permits deleting it.
package relations

import (
	"context"
	"fmt"
	"log"
	"time"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/authzed/authzed-go/v1"
	"github.com/authzed/grpcutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// schema is written to SpiceDB at startup (idempotent — writing the same
// schema again is a no-op).
const schema = `
definition user {}

definition record {
	relation owner: user
	permission delete = owner
}
`

// Client talks to SpiceDB.
type Client struct {
	sc *authzed.Client
}

// Connect dials SpiceDB, writes the schema, and seeds the demo
// relationships. Retries while the container finishes booting.
func Connect(ctx context.Context, endpoint, token string) (*Client, error) {
	sc, err := authzed.NewClient(
		endpoint,
		grpcutil.WithInsecureBearerToken(token),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial spicedb: %w", err)
	}
	c := &Client{sc: sc}

	for attempt := 1; attempt <= 15; attempt++ {
		_, err = sc.WriteSchema(ctx, &v1.WriteSchemaRequest{Schema: schema})
		if err == nil {
			break
		}
		log.Printf("relations: spicedb not ready (attempt %d): %v", attempt, err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("write spicedb schema: %w", err)
	}

	// Demo seed: alice owns records 1 and 2 — she does NOT own record 3,
	// so even as admin she cannot delete it. TOUCH makes re-seeding a no-op.
	for _, rec := range []string{"1", "2"} {
		if err := c.Grant(ctx, "alice", rec); err != nil {
			return nil, fmt.Errorf("seed relationships: %w", err)
		}
	}
	return c, nil
}

// CheckOwner reports whether user may delete the record, per the graph.
// Fully-consistent read: a revocation a millisecond ago must count — the
// whole point of the TOCTOU re-check.
func (c *Client) CheckOwner(ctx context.Context, user, recordID string) (bool, error) {
	resp, err := c.sc.CheckPermission(ctx, &v1.CheckPermissionRequest{
		Consistency: &v1.Consistency{
			Requirement: &v1.Consistency_FullyConsistent{FullyConsistent: true},
		},
		Resource:   &v1.ObjectReference{ObjectType: "record", ObjectId: recordID},
		Permission: "delete",
		Subject: &v1.SubjectReference{
			Object: &v1.ObjectReference{ObjectType: "user", ObjectId: user},
		},
	})
	if err != nil {
		return false, fmt.Errorf("spicedb check: %w", err)
	}
	return resp.Permissionship == v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION, nil
}

// Grant writes user -> owner -> record (TOUCH: idempotent).
func (c *Client) Grant(ctx context.Context, user, recordID string) error {
	return c.write(ctx, v1.RelationshipUpdate_OPERATION_TOUCH, user, recordID)
}

// Revoke deletes the ownership relationship.
func (c *Client) Revoke(ctx context.Context, user, recordID string) error {
	return c.write(ctx, v1.RelationshipUpdate_OPERATION_DELETE, user, recordID)
}

func (c *Client) write(ctx context.Context, op v1.RelationshipUpdate_Operation, user, recordID string) error {
	_, err := c.sc.WriteRelationships(ctx, &v1.WriteRelationshipsRequest{
		Updates: []*v1.RelationshipUpdate{{
			Operation: op,
			Relationship: &v1.Relationship{
				Resource: &v1.ObjectReference{ObjectType: "record", ObjectId: recordID},
				Relation: "owner",
				Subject: &v1.SubjectReference{
					Object: &v1.ObjectReference{ObjectType: "user", ObjectId: user},
				},
			},
		}},
	})
	return err
}

// Ownership is one edge in the graph, for display.
type Ownership struct {
	User     string `json:"user"`
	RecordID string `json:"record_id"`
}

// List returns every owner edge currently in the graph.
func (c *Client) List(ctx context.Context) ([]Ownership, error) {
	stream, err := c.sc.ReadRelationships(ctx, &v1.ReadRelationshipsRequest{
		Consistency: &v1.Consistency{
			Requirement: &v1.Consistency_FullyConsistent{FullyConsistent: true},
		},
		RelationshipFilter: &v1.RelationshipFilter{
			ResourceType:     "record",
			OptionalRelation: "owner",
		},
	})
	if err != nil {
		return nil, err
	}
	var out []Ownership
	for {
		r, err := stream.Recv()
		if err != nil {
			break // io.EOF ends the stream; other errors also just end the list
		}
		out = append(out, Ownership{
			User:     r.Relationship.Subject.Object.ObjectId,
			RecordID: r.Relationship.Resource.ObjectId,
		})
	}
	return out, nil
}
