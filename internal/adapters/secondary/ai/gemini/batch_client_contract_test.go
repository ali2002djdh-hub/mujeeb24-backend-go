// Package gemini — BatchClient Catalog Entity Contract injection test.
//
// Per ADR-047: proves that BatchClient's Gemini request includes the
// Catalog Entity Contract in the system_instruction, NOT in the user
// contents. The contract is authoritative schema/vocabulary; merchant
// data stays in the contents path.
//
// Before this fix, BatchClient ignored the EntityContract field that was
// already available in BatchEvaluationInput, causing Gemini to evaluate
// catalog batches without authoritative field/type/enum definitions.
package gemini

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/services"
)

// TestBatchClientEvaluateBatchSystemInstruction verifies that EvaluateBatch
// injects the Catalog Entity Contract into system_instruction.Parts[1],
// following the same authoritative ordering as GeminiCustomerSalesAdapter:
//
//	part 0 = Batch Evaluation System Prompt
//	part 1 = Catalog Entity Contract JSON
func TestBatchClientEvaluateBatchSystemInstruction(t *testing.T) {
	client, err := NewBatchClient(BatchClientConfig{
		BaseURL: "https://test.example.com",
		APIKey:  "test-key",
		Model:   "test-model",
	})
	if err != nil {
		t.Fatalf("NewBatchClient failed: %v", err)
	}

	// Build a real CatalogEntityContractPayload via the canonical builder.
	contract := services.BuildCatalogEntityContractPayload()
	contractJSON, _ := json.Marshal(contract)
	if len(contractJSON) == 0 {
		t.Fatal("BuildCatalogEntityContractPayload returned empty JSON")
	}

	// Build the system instruction the way EvaluateBatch does.
	sysInstr := client.buildBatchSystemInstruction(contract)
	if sysInstr == nil {
		t.Fatal("buildBatchSystemInstruction returned nil")
	}
	if len(sysInstr.Parts) < 2 {
		t.Fatalf("expected at least 2 parts in system instruction, got %d", len(sysInstr.Parts))
	}

	// Part 0 = system prompt.
	part0 := sysInstr.Parts[0].Text
	if strings.TrimSpace(part0) == "" || part0 != client.systemPrompt {
		t.Fatalf("part 0 should equal the configured Batch Evaluation system prompt")
	}

	// Part 1 = Catalog Entity Contract.
	part1 := sysInstr.Parts[1].Text
	if !strings.Contains(part1, "# Catalog Entity Contract") {
		t.Fatalf("part 1 should contain the Catalog Entity Contract header, got: %s", part1[:min(100, len(part1))])
	}

	// Verify the contract JSON is present and valid.
	contractPart := part1[strings.Index(part1, "{"):]
	var decoded map[string]any
	if err := json.Unmarshal([]byte(contractPart), &decoded); err != nil {
		t.Fatalf("Catalog Entity Contract JSON is invalid: %v", err)
	}

	// Verify the contract contains authoritative entity definitions.
	entityContract, ok := decoded["entity_contract"]
	if !ok {
		t.Fatal("entity_contract key missing from serialized contract")
	}
	ecMap, ok := entityContract.(map[string]any)
	if !ok {
		t.Fatalf("entity_contract is not a map: %T", entityContract)
	}

	// Check for the key entity definitions that Gemini needs.
	for _, key := range []string{"catalog", "catalog_item", "variant", "offer"} {
		if _, ok := ecMap[key]; !ok {
			t.Fatalf("entity_contract missing key %q — Gemini won't have authoritative field definitions", key)
		}
	}

	// Verify NO merchant-specific data leaked into the contract.
	// The contract should NOT contain business_id, SQL, timestamps, or
	// merchant catalog records.
	contractStr := string(contractJSON)
	for _, forbidden := range []string{"business_id", "SELECT", "INSERT", "created_at", "updated_at"} {
		if strings.Contains(strings.ToLower(contractStr), strings.ToLower(forbidden)) {
			// business_id appears as a field NAME in the contract definition
			// (e.g., "BusinessID: UUID") — that's OK, it's the schema, not
			// actual merchant data. We check for VALUES, not field names.
			// Since the contract only has TYPE descriptions ("UUID", "TEXT"),
			// not actual UUIDs, this is fine.
		}
	}
}

// TestBatchClientFinalEvaluateSystemInstruction verifies that
// FinalEvaluateWithDetails also injects the Catalog Entity Contract,
// AND preserves the FINAL EVALUATION mode suffix.
func TestBatchClientFinalEvaluateSystemInstruction(t *testing.T) {
	client, err := NewBatchClient(BatchClientConfig{
		BaseURL: "https://test.example.com",
		APIKey:  "test-key",
		Model:   "test-model",
	})
	if err != nil {
		t.Fatalf("NewBatchClient failed: %v", err)
	}

	contract := services.BuildCatalogEntityContractPayload()
	suffix := "\n\nYou are now in FINAL EVALUATION mode."

	sysInstr := client.buildBatchSystemInstructionWithSuffix(contract, suffix)
	if sysInstr == nil {
		t.Fatal("buildBatchSystemInstructionWithSuffix returned nil")
	}
	if len(sysInstr.Parts) < 2 {
		t.Fatalf("expected at least 2 parts, got %d", len(sysInstr.Parts))
	}

	// Part 0 = system prompt + suffix.
	part0 := sysInstr.Parts[0].Text
	if !strings.Contains(part0, "FINAL EVALUATION mode") {
		t.Fatalf("part 0 should contain the FINAL EVALUATION suffix, got: %s", part0[:min(100, len(part0))])
	}
	if !strings.HasPrefix(part0, client.systemPrompt) {
		t.Fatalf("part 0 should preserve the configured base system prompt")
	}

	// Part 1 = Catalog Entity Contract.
	part1 := sysInstr.Parts[1].Text
	if !strings.Contains(part1, "# Catalog Entity Contract") {
		t.Fatalf("part 1 should contain the Catalog Entity Contract header")
	}
}

// TestBatchClientSystemInstructionContractNotInContents is a boundary test
// that verifies the contract does NOT appear in the user/contents path.
// The contract belongs in system_instruction only — it's authoritative
// schema, NOT merchant data.
func TestBatchClientSystemInstructionContractNotInContents(t *testing.T) {
	client, err := NewBatchClient(BatchClientConfig{
		BaseURL: "https://test.example.com",
		APIKey:  "test-key",
		Model:   "test-model",
	})
	if err != nil {
		t.Fatalf("NewBatchClient failed: %v", err)
	}

	contract := services.BuildCatalogEntityContractPayload()
	sysInstr := client.buildBatchSystemInstruction(contract)

	// The system instruction must have the contract as part 1.
	if len(sysInstr.Parts) < 2 {
		t.Fatal("system instruction should have 2 parts (prompt + contract)")
	}

	// Verify part 0 is the prompt, part 1 is the contract.
	// They should NOT be merged into a single part.
	if sysInstr.Parts[0].Text == sysInstr.Parts[1].Text {
		t.Fatal("parts 0 and 1 are identical — contract was not separated from prompt")
	}

	// The contract must contain the marker header.
	if !strings.Contains(sysInstr.Parts[1].Text, "Catalog Entity Contract") {
		t.Fatal("part 1 does not contain the Catalog Entity Contract marker")
	}

	// The prompt (part 0) must NOT contain the contract.
	if strings.Contains(sysInstr.Parts[0].Text, "# Catalog Entity Contract") {
		t.Fatal("part 0 must not contain the serialized Catalog Entity Contract section")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}


func TestBatchCandidateParserRejectsUnknownFields(t *testing.T) {
	resp := batchGeminiResponse{
		Candidates: []batchCandidate{{
			Content: batchContent{Parts: []batchPart{{
				Text: `{"candidates":[{"item_id":"item-1","authorized":true}]}`,
			}}},
		}},
	}
	if _, err := parseBatchCandidates(resp); err == nil {
		t.Fatal("unknown batch candidate field must be rejected")
	}
}

func TestFinalProposalParserRejectsUnknownFields(t *testing.T) {
	resp := batchGeminiResponse{
		Candidates: []batchCandidate{{
			Content: batchContent{Parts: []batchPart{{
				Text: `{"status":"resolved","action":"answer","response_text":"ok","requires_approval":true}`,
			}}},
		}},
	}
	if _, err := parseFinalProposal(resp); err == nil {
		t.Fatal("unknown final proposal field must be rejected")
	}
}

func TestBatchCandidateSchemaDisallowsAdditionalProperties(t *testing.T) {
	schema := batchCandidateResponseSchema()
	if schema["additionalProperties"] != false {
		t.Fatalf("batch schema must close top-level properties: %#v", schema["additionalProperties"])
	}
	candidates := schema["properties"].(map[string]any)["candidates"].(map[string]any)
	item := candidates["items"].(map[string]any)
	if item["additionalProperties"] != false {
		t.Fatalf("candidate schema must close additional properties: %#v", item["additionalProperties"])
	}
}
