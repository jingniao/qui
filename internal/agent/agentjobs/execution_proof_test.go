package agentjobs

import (
	"testing"
)

func TestSuccessfulOutcomesRequireDurableExecutionBinding(t *testing.T) {
	for _, status := range []string{"success", "rolled_back"} {
		t.Run(status, func(t *testing.T) {
			o, request := fixture(t)
			store := openTest(t, o)
			if err := store.Queue(request); err != nil {
				t.Fatal(err)
			}
			result := success(request)
			result.Status = status
			result.Code = status
			before, err := Fingerprint(o)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Finish(request, result); err == nil {
				t.Fatal("unexecuted request became success")
			}
			after, err := Fingerprint(o)
			if err != nil || before != after {
				t.Fatal("rejected success changed evidence", err)
			}
			if err := store.MarkStarted(request); err != nil {
				t.Fatal(err)
			}
			if err := store.Finish(request, result); err != nil {
				t.Fatal(err)
			}
			store.Close()
			store = openTest(t, o)
			defer store.Close()
			job, found := store.Get(request.ID)
			if !found || !job.Started || job.Result == nil || job.Result.Status != status {
				t.Fatal("execution/result did not survive", job)
			}
		})
	}
}

func TestAuthenticatedOutcomeWithoutStartIsRejectedOnReopen(t *testing.T) {
	o, request := fixture(t)
	store := openTest(t, o)
	if err := store.Queue(request); err != nil {
		t.Fatal(err)
	}
	result := success(request)
	// Build an authenticated but semantically invalid tail, as might be left by
	// an older faulty writer. Authentication alone must not establish execution.
	e := event{Kind: "agent-job-result-v1", NodeID: o.NodeID, Number: 2, Previous: store.head.Digest, Request: request, Result: &result}
	data, err := store.seal(e)
	if err != nil {
		t.Fatal(err)
	}
	if err := exclusive(store.root, eventName(2), data); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if reopened, err := Open(o); err == nil {
		reopened.Close()
		t.Fatal("invalid successful tail repaired as committed")
	}
}
