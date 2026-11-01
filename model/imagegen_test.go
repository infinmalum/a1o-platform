package model

import (
	"strings"
	"testing"
)

func openImageGenTestDB(t *testing.T) {
	t.Helper()
	previous := DB
	db := openAgentRunTestDB(t)
	if err := db.AutoMigrate(&ImageGeneration{}, &ImageAsset{}); err != nil {
		t.Fatal(err)
	}
	DB = db
	t.Cleanup(func() { DB = previous })
}

func TestImageGenerationIdempotentReplayAndConflict(t *testing.T) {
	openImageGenTestDB(t)
	first := &ImageGeneration{UserId: 7, Model: "dall-e-3", Prompt: "一只猫",
		Size: "1024x1024", N: 1, IdempotencyKey: "gen-1"}
	existing, created, err := CreateImageGeneration(first)
	if err != nil || !created {
		t.Fatalf("first insert: created=%v err=%v", created, err)
	}
	if existing.Status != ImageGenStatusQueued {
		t.Fatalf("new generation must be queued, got %s", existing.Status)
	}
	replay, created, err := CreateImageGeneration(&ImageGeneration{UserId: 7, Model: "dall-e-3",
		Prompt: "一只猫", Size: "1024x1024", N: 1, IdempotencyKey: "gen-1"})
	if err != nil || created || replay.Id != existing.Id {
		t.Fatalf("same request must replay the row: created=%v err=%v id=%d/%d", created, err, replay.Id, existing.Id)
	}
	if _, _, err := CreateImageGeneration(&ImageGeneration{UserId: 7, Model: "dall-e-3",
		Prompt: "一只狗", Size: "1024x1024", N: 1, IdempotencyKey: "gen-1"}); !strings.Contains(err.Error(), "幂等键") {
		t.Fatalf("different prompt must conflict, got %v", err)
	}
}

func TestImageGenerationClaimIsSingleWinner(t *testing.T) {
	openImageGenTestDB(t)
	for i := 0; i < 2; i++ {
		if _, _, err := CreateImageGeneration(&ImageGeneration{UserId: 7, Model: "dall-e-2",
			Prompt: "p", Size: "1024x1024", N: 1, IdempotencyKey: string(rune('a' + i))}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := ClaimQueuedImageGeneration()
	if err != nil || first == nil {
		t.Fatalf("claim: %v", err)
	}
	second, err := ClaimQueuedImageGeneration()
	if err != nil || second == nil {
		t.Fatalf("second claim: %v", err)
	}
	if first.Id == second.Id {
		t.Fatal("two claims returned the same row")
	}
	third, err := ClaimQueuedImageGeneration()
	if err != nil || third != nil {
		t.Fatalf("queue must be empty now: %v %v", third, err)
	}
}

func TestImageGenerationCancelAndReconcile(t *testing.T) {
	openImageGenTestDB(t)
	queued, _, err := CreateImageGeneration(&ImageGeneration{UserId: 7, Model: "dall-e-2",
		Prompt: "p", Size: "1024x1024", N: 1, IdempotencyKey: "cancel-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := CancelQueuedImageGeneration(queued.Id, 7); err != nil {
		t.Fatalf("queued cancel: %v", err)
	}
	if err := CancelQueuedImageGeneration(queued.Id, 7); err == nil {
		t.Fatal("cancelling a cancelled row must fail")
	}
	running, _, err := CreateImageGeneration(&ImageGeneration{UserId: 7, Model: "dall-e-2",
		Prompt: "p", Size: "1024x1024", N: 1, IdempotencyKey: "running-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimQueuedImageGeneration(); err != nil {
		t.Fatal(err)
	}
	if err := CancelQueuedImageGeneration(running.Id, 7); err == nil {
		t.Fatal("running rows cannot be cancelled")
	}
	stuck, err := ClaimQueuedImageGeneration()
	if err != nil || stuck != nil {
		t.Fatalf("nothing left queued: %v %v", stuck, err)
	}
	// Simulate a crashed process leaving a running row, then reconcile.
	if err := DB.Model(&ImageGeneration{Id: running.Id}).Update("status", ImageGenStatusRunning).Error; err != nil {
		t.Fatal(err)
	}
	moved, err := ReconcileStuckImageGenerations()
	if err != nil || moved != 1 {
		t.Fatalf("reconcile: moved=%d err=%v", moved, err)
	}
	stored, err := GetImageGeneration(running.Id, 7)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != ImageGenStatusNeedsReconciliation {
		t.Fatalf("stuck row must need reconciliation, got %s", stored.Status)
	}
}

func TestImageAssetSlots(t *testing.T) {
	openImageGenTestDB(t)
	gen, _, err := CreateImageGeneration(&ImageGeneration{UserId: 7, Model: "dall-e-2",
		Prompt: "p", Size: "1024x1024", N: 2, IdempotencyKey: "slots-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := CreateImageAsset(&ImageAsset{UserId: 7, RunId: gen.Id, Slot: 0, Mime: "image/png"}); err != nil {
		t.Fatal(err)
	}
	slots, err := GetRunImageAssetSlots(gen.Id, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !slots[0] || slots[1] {
		t.Fatalf("slot map wrong: %v", slots)
	}
	if _, err := GetImageAsset(1, 8); err == nil {
		t.Fatal("foreign user must not read the asset")
	}
}
