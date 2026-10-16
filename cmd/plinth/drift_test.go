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

// Shaped like the Docker provider right after apply: empty settings stored as
// null are read back as [] / {} / 0. That is not drift.
const normalizationJSON = `{"resource_drift":[
 {"address":"module.application__whoami.docker_container.this","module_address":"module.application__whoami",
  "type":"docker_container","name":"this",
  "change":{"actions":["update"],
   "before":{"dns":null,"log_opts":null,"networks_advanced":[{"name":"n","gw_priority":null}],"must_run":true,"ports":[{"external":18081}]},
   "after": {"dns":[],"log_opts":{},"networks_advanced":[{"name":"n","gw_priority":0}],"must_run":true,"ports":[{"external":18081}]}}},
 {"address":"module.network__main.docker_network.this","module_address":"module.network__main",
  "type":"docker_network","name":"this",
  "change":{"actions":["update"],"before":{"ipam_options":null},"after":{"ipam_options":{}}}}
]}`

func TestDriftIgnoresProviderNormalization(t *testing.T) {
	report, err := driftReport([]byte(normalizationJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(report) != 0 {
		t.Fatalf("normalization reported as drift:\n%s", strings.Join(report, "\n"))
	}

	// A real change next to the noise is still reported, without the noise.
	real := strings.Replace(normalizationJSON, `"must_run":true,"ports":[{"external":18081}]}}}`, `"must_run":false,"ports":[]}}}`, 1)
	report, _ = driftReport([]byte(real))
	got := strings.Join(report, "\n")
	for _, want := range []string{"~ application/whoami  docker_container.this", "must_run: true → false", `ports: [{"external":18081}] → []`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, noise := range []string{"dns", "log_opts", "gw_priority", "network/main"} {
		if strings.Contains(got, noise) {
			t.Errorf("noise %q reported:\n%s", noise, got)
		}
	}
}
