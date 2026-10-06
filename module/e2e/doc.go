// Package e2e draagt de boot-e2e van de duifmodule: hij start de echte
// server-binary tegen een wegwerp-Postgres en een OpenAI-compatibele stub-LLM
// en controleert het wire-contract. De tests staan achter de build-tag `e2e`
// zodat `make test` docker- en procesvrij blijft; draai ze met `make e2e`.
package e2e
