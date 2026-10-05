package services

import (
	"context"
	"testing"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

type stubCustomerSalesPolicy struct {
	decision ports.CustomerSalesPolicyDecision
}

func (s stubCustomerSalesPolicy) Evaluate(context.Context, ports.CustomerSalesProposal, *ports.CustomerSalesContext) ports.CustomerSalesPolicyDecision {
	return s.decision
}

func TestPolicyDecisionEmptyRequiresApproval(t *testing.T) {
	pipeline := NewValidationPipeline(nil, nil, stubCustomerSalesPolicy{
		decision: ports.CustomerSalesPolicyDecision{},
	}, nil)

	decision, failure := pipeline.evaluateCustomerSalesPolicy(context.Background(), ValidationInput{
		Proposal: ports.CustomerSalesProposal{Action: ports.CustomerSalesProposalActionAnswer},
	})
	if failure != nil {
		t.Fatalf("unexpected failure: %v", failure)
	}
	if decision.PolicyDecision != "requires_approval" {
		t.Fatalf("PolicyDecision=%q, want requires_approval", decision.PolicyDecision)
	}
}

func TestPolicyDecisionUnknownRequiresApproval(t *testing.T) {
	pipeline := NewValidationPipeline(nil, nil, stubCustomerSalesPolicy{
		decision: ports.CustomerSalesPolicyDecision{Decision: "unexpected"},
	}, nil)

	decision, failure := pipeline.evaluateCustomerSalesPolicy(context.Background(), ValidationInput{
		Proposal: ports.CustomerSalesProposal{Action: ports.CustomerSalesProposalActionAnswer},
	})
	if failure != nil {
		t.Fatalf("unexpected failure: %v", failure)
	}
	if decision.PolicyDecision != "requires_approval" {
		t.Fatalf("PolicyDecision=%q, want requires_approval", decision.PolicyDecision)
	}
}

func TestStructuredRoutingReasonValidation(t *testing.T) {
	pipeline := &ValidationPipeline{}
	valid := ports.CustomerSalesProposal{
		Status:        ports.CustomerSalesProposalStatusResolved,
		Action:        ports.CustomerSalesProposalActionHumanRequest,
		RoutingReason: ports.CustomerSalesRoutingReasonSubscriptionActivation,
	}
	if failure := pipeline.validateStructural(valid); failure != nil {
		t.Fatalf("valid routing reason rejected: %v", failure)
	}

	invalid := valid
	invalid.RoutingReason = ports.CustomerSalesRoutingReason("invented_reason")
	if failure := pipeline.validateStructural(invalid); failure == nil {
		t.Fatal("unknown routing reason must be rejected")
	}
}
