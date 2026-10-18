# Write=2 agent store fixture generator

`testdata/store_write2/` is an agent store written in the current (`Write=2`)
identity format by the pre-refactor code at commit `0c619ba`. It lets
`internal/agent/identity/store_fixture_test.go` prove that the collapsed
single-CA code opens the store unchanged (same node ID and certificate) and that
a store using a removed identity format is refused with a re-enroll error.

The store contains only test-generated keys and no real secret.

## Regenerate

Copy the generator into the identity package, run it once with the output
variable set, then remove the copy:

```sh
export PATH="$PWD/.tools/go/bin:$PATH" \
       GOCACHE="$PWD/.tools/gocache" \
       GOPATH="$PWD/.tools/gopath" \
       GOTOOLCHAIN=local
cp internal/agent/identity/testdata/generate_store/generate_store_fixture_test.go \
   internal/agent/identity/zz_generate_fixture_test.go
STORE_FIXTURE_OUT="$PWD/internal/agent/identity/testdata/store_write2" \
  go test ./internal/agent/identity -run '^TestGenerateWrite2StoreFixture$' -count=1 -v
rm internal/agent/identity/zz_generate_fixture_test.go
```

The certificate fingerprint is asserted in the fixture test; update
`fixtureCertFingerprint` in `store_fixture_test.go` if the fixture is
regenerated.
