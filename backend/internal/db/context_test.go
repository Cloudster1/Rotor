package db

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestAContextNamesNobodyUntilAPersonIsGiven(t *testing.T) {
	if _, ok := UserFrom(context.Background()); ok {
		t.Fatal("a bare context named somebody")
	}
	if _, ok := UserFrom(WithUser(context.Background(), uuid.Nil)); ok {
		t.Fatal("the nil id counted as a person")
	}
	id := uuid.New()
	got, ok := UserFrom(WithUser(context.Background(), id))
	if !ok || got != id {
		t.Fatalf("UserFrom = %v, %v; want %v", got, ok, id)
	}
}

func TestNamingAnotherPersonReplacesTheFirst(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	ctx := WithUser(WithUser(context.Background(), first), second)
	if got, _ := UserFrom(ctx); got != second {
		t.Fatalf("UserFrom = %v, want %v", got, second)
	}
}
