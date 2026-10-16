package main

import (
	"strings"
	"testing"
)

// Shaped like `tofu show -json` of a refresh-only plan (OpenTofu 1.13).
const driftJSON = `{"resource_drift":[
 {"address":"module.application__checkout-api.terraform_data.deployment",
  "module_address":"module.application__checkout-api","type":"terraform_data","name":"deployment",
  "change":{"actions":["update"],
   "before":{"output":{"replicas":5,"env":{"LOG_LEVEL":"info"},"ports":[80]},"password":"hunter2"},
   "after": {"output":{"replicas":12,"env":{"LOG_LEVEL":"debug"},"ports":[80,443]},"password":"s3cret!"},
   "before_sensitive":{"password":true},"after_sensitive":{"password":true}}},
 {"address":"module.network__main.module.inner.aws_vpc.this",
  "module_address":"module.network__main.module.inner","type":"aws_vpc","name":"this",
  "change":{"actions":["delete"],"before":{"id":"vpc-1"},"after":null}},
 {"address":"local_file.root","module_address":"","type":"local_file","name":"root",
  "change":{"actions":["update"],"before":{"content":"a"},"after":{"content":"b"}}}
]}`

func TestDriftReport(t *testing.T) {
	report, err := driftReport([]byte(driftJSON))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(report, "\n")
	for _, want := range []string{
		"~ application/checkout-api  terraform_data.deployment",
		"    output.replicas: 5 → 12",
		`    output.env.LOG_LEVEL: "info" → "debug"`,
		"    output.ports: [80] → [80,443]",
		"    password: (sensitive value changed)",
		"- network/main  aws_vpc.this: deleted outside Plinth", // nested module → top-level call
		"~ local_file.root  local_file.root",                   // not a Plinth module: raw address
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q\n%s", want, got)
		}
	}
	for _, secret := range []string{"hunter2", "s3cret!"} {
		if strings.Contains(got, secret) {
			t.Errorf("sensitive value %q leaked:\n%s", secret, got)
		}
	}
}

func TestInstanceAddr(t *testing.T) {
	for in, want := range map[string]string{
		"module.application__checkout-api":        "application/checkout-api",
		"module.network__main.module.inner":       "network/main",
		"module.database__orders-2":               "database/orders-2",
		`module.kubernetes_cluster__default["a"]`: "kubernetes_cluster/default",
		"module.something":                        "fallback",
		"":                                        "fallback",
	} {
		if got := instanceAddr(in, "fallback"); got != want {
			t.Errorf("instanceAddr(%q) = %q, want %q", in, got, want)
		}
	}
}
