package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	logging "github.com/ipfs/go-log/v2"
)

func TestRunCommand(t *testing.T) {
	testCases := []struct {
		name    string
		command string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:    "without input",
			command: "echo -n",
			input:   "",
			want:    "",
			wantErr: false,
		},
		{
			name:    "with input",
			command: "cat",
			input:   "Hello",
			want:    "Hello",
			wantErr: false,
		},
		{
			name:    "invalid command",
			command: "nonexistentcommand",
			input:   "",
			want:    "",
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := runCommand(tc.command, tc.input)
			if (err != nil) != tc.wantErr {
				t.Errorf("runCommand(%q, %q) error = %v, wantErr %v", tc.command, tc.input, err, tc.wantErr)
				return
			}
			if !tc.wantErr && strings.TrimSpace(got) != tc.want {
				t.Errorf("runCommand(%q, %q) = %q, want %q", tc.command, tc.input, got, tc.want)
			}
		})
	}
}

func TestRunCommandCapturesStderr(t *testing.T) {
	_, _, err := runCommand("sh -c echo_boom_and_fail", "")
	if err == nil {
		t.Fatal("expected an error for a missing command")
	}

	// The verifier reports why it gave up on stderr and signals what went wrong
	// through the exit status; both have to survive into the returned error.
	_, _, err = runCommand("sh -c", "")
	if err == nil {
		t.Fatal("expected an error when sh -c is given no script")
	}
	if !strings.Contains(err.Error(), "exit status") {
		t.Errorf("error should carry the exit status, got: %v", err)
	}
	if !strings.Contains(err.Error(), "sh:") && !strings.Contains(err.Error(), "option") {
		t.Errorf("error should carry the stderr text, got: %v", err)
	}
}

func TestParseDelegationVerifyOutput(t *testing.T) {
	const (
		validA     = `{"submitted_at_date":"2026-08-27","submitter":"A","verified":true}`
		validB     = `{"submitted_at_date":"2026-08-27","submitter":"B","verified":true}`
		logLine    = `2026-08-27 10:00:00 INFO starting verification`
		jsonLog    = `{"level":"info","message":"loaded config"}`
		badRecord  = `{"submitted_at_date":12345}`
		truncated  = `{"submitted_at_date":"2026-08-27","submitter":`
		emptyLines = "\n\n   \n"
	)

	testCases := []struct {
		name          string
		data          string
		wantSubmitter []string
		wantMalformed int
	}{
		{
			name:          "only valid records",
			data:          validA + "\n" + validB,
			wantSubmitter: []string{"A", "B"},
		},
		{
			name:          "log lines are skipped quietly",
			data:          logLine + "\n" + validA + "\n" + jsonLog + "\n" + validB + emptyLines,
			wantSubmitter: []string{"A", "B"},
		},
		{
			name:          "a malformed record does not cost the valid ones",
			data:          validA + "\n" + badRecord + "\n" + validB,
			wantSubmitter: []string{"A", "B"},
			wantMalformed: 1,
		},
		{
			name:          "a truncated record is reported, not silently dropped",
			data:          validA + "\n" + truncated + "\n" + validB,
			wantSubmitter: []string{"A", "B"},
			wantMalformed: 1,
		},
		{
			name:          "nothing parseable",
			data:          badRecord,
			wantSubmitter: nil,
			wantMalformed: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			submissions, malformed := parseDelegationVerifyOutput(tc.data)

			var got []string
			for _, sub := range submissions {
				got = append(got, sub.Submitter)
			}
			if strings.Join(got, ",") != strings.Join(tc.wantSubmitter, ",") {
				t.Errorf("submitters = %v, want %v", got, tc.wantSubmitter)
			}
			if len(malformed) != tc.wantMalformed {
				t.Errorf("malformed = %d (%v), want %d", len(malformed), malformed, tc.wantMalformed)
			}
		})
	}
}

// writeStubPrinter creates an executable that drains stdin and prints the given
// lines, standing in for the delegation-verify binary.
func writeStubPrinter(t *testing.T, lines ...string) string {
	t.Helper()

	script := "#!/bin/sh\ncat > /dev/null\n"
	for _, line := range lines {
		script += "cat <<'RECORD'\n" + line + "\nRECORD\n"
	}

	path := filepath.Join(t.TempDir(), "delegation-verify-stub")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing stub: %v", err)
	}
	return path
}

func testAppContext() *AppContext {
	return &AppContext{Log: logging.Logger("submission-updater-test")}
}

func TestRunDelegationVerifyCommandIsolatesBadRecords(t *testing.T) {
	// One node's unparseable record must not discard the records belonging to
	// every other node in the same batch.
	stub := writeStubPrinter(t,
		`{"submitted_at_date":"2026-08-27","submitter":"A","verified":true}`,
		`starting verification`,
		`{"submitted_at_date":12345}`,
		`{"submitted_at_date":"2026-08-27","submitter":"B","verified":true}`,
	)

	submissions, err := testAppContext().runDelegationVerifyCommand(stub, "", "[]")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(submissions) != 2 {
		t.Fatalf("got %d submissions, want 2 (%+v)", len(submissions), submissions)
	}
	if submissions[0].Submitter != "A" || submissions[1].Submitter != "B" {
		t.Errorf("got submitters %q and %q, want A and B", submissions[0].Submitter, submissions[1].Submitter)
	}
}

func TestRunDelegationVerifyCommandFailsWhenNothingParses(t *testing.T) {
	// Losing every record is the verifier misbehaving rather than one bad
	// submitter, and must not be mistaken for an empty batch.
	stub := writeStubPrinter(t, `{"submitted_at_date":12345}`)

	submissions, err := testAppContext().runDelegationVerifyCommand(stub, "", "[]")
	if err == nil {
		t.Fatalf("expected an error, got %d submissions", len(submissions))
	}
	if !strings.Contains(err.Error(), "could be parsed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunDelegationVerifyCommandReportsVerifierStderr(t *testing.T) {
	path := filepath.Join(t.TempDir(), "failing-verify")
	script := "#!/bin/sh\ncat > /dev/null\necho '{\"error\":\"fail to read config file\"}' >&2\nexit 4\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing stub: %v", err)
	}

	_, err := testAppContext().runDelegationVerifyCommand(path, "", "[]")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "fail to read config file") {
		t.Errorf("error should carry the verifier's stderr, got: %v", err)
	}
	if !strings.Contains(err.Error(), "exit status 4") {
		t.Errorf("error should carry the exit status, got: %v", err)
	}
}

func TestRunDelegationVerifyCommandFailsOnEmptyOutput(t *testing.T) {
	// A verifier that returns nothing for a non-empty batch has failed; treating
	// that as an empty batch would quietly update no submissions and exit 0.
	stub := writeStubPrinter(t)

	if _, err := testAppContext().runDelegationVerifyCommand(stub, "", "[]"); err == nil {
		t.Fatal("expected an error when the verifier returns no records")
	}
}

func TestRunCommandKeepsOutputWrittenBeforeFailure(t *testing.T) {
	// Records emitted before the process died belong to nodes that did nothing
	// wrong, so the caller must still be able to see them.
	path := filepath.Join(t.TempDir(), "dies-partway")
	script := "#!/bin/sh\ncat > /dev/null\necho 'partial record'\necho 'boom' >&2\nexit 2\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing stub: %v", err)
	}

	stdout, stderr, err := runCommand(path, "")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(stdout, "partial record") {
		t.Errorf("stdout written before the failure was dropped, got %q", stdout)
	}
	if !strings.Contains(stderr, "boom") {
		t.Errorf("stderr was dropped, got %q", stderr)
	}
}

func TestBoundedBufferKeepsTail(t *testing.T) {
	b := &boundedBuffer{max: 8}
	if n, err := b.Write([]byte("0123456789abcdef")); n != 16 || err != nil {
		t.Fatalf("Write returned (%d, %v), want (16, nil)", n, err)
	}
	if got := b.String(); got != "(truncated) ...89abcdef" {
		t.Errorf("String() = %q, want the last 8 bytes marked truncated", got)
	}

	small := &boundedBuffer{max: 8}
	small.Write([]byte("hi\n"))
	if got := small.String(); got != "hi" {
		t.Errorf("String() = %q, want %q unmarked", got, "hi")
	}
}

func TestPartitionSubmissionsByCutover(t *testing.T) {
	cutover := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	submissions := []Submission{
		{ID: "before", SubmittedAt: cutover.Add(-time.Hour)},
		{ID: "just-before", SubmittedAt: cutover.Add(-time.Second)},
		{ID: "at-cutover", SubmittedAt: cutover},
		{ID: "after", SubmittedAt: cutover.Add(time.Hour)},
	}

	preFork, postFork := partitionSubmissionsByCutover(submissions, cutover)

	wantPreFork := []string{"before", "just-before"}
	wantPostFork := []string{"at-cutover", "after"}

	if len(preFork) != len(wantPreFork) {
		t.Fatalf("partitionSubmissionsByCutover() pre-fork size = %v, want %v", len(preFork), len(wantPreFork))
	}
	for i, sub := range preFork {
		if sub.ID != wantPreFork[i] {
			t.Errorf("partitionSubmissionsByCutover() pre-fork[%d] = %v, want %v", i, sub.ID, wantPreFork[i])
		}
	}
	if len(postFork) != len(wantPostFork) {
		t.Fatalf("partitionSubmissionsByCutover() post-fork size = %v, want %v", len(postFork), len(wantPostFork))
	}
	for i, sub := range postFork {
		if sub.ID != wantPostFork[i] {
			t.Errorf("partitionSubmissionsByCutover() post-fork[%d] = %v, want %v", i, sub.ID, wantPostFork[i])
		}
	}
}

// writeStubVerifier creates a stub delegation-verify executable that records
// the arguments it was invoked with and echoes each input submission back on
// its own line, tagging validation_error with the given marker so tests can
// tell which stub processed each submission.
func writeStubVerifier(t *testing.T, dir, name, marker string) (binPath, argsFile string) {
	t.Helper()
	binPath = filepath.Join(dir, name)
	argsFile = binPath + ".args"
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s ' "$@" > %q
sed -e 's/^\[//' -e 's/\]$//' -e 's/"validation_error":""/"validation_error":"%s"/g' -e 's/},{/}\
{/g'
`, argsFile, marker)
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("error writing stub verifier %v: %v", name, err)
	}
	return binPath, argsFile
}

func newTestAppContext(cutover time.Time, preBin, postBin string) *AppContext {
	return &AppContext{
		AppConfig: AppConfig{
			DelegationVerifyBinPath:         preBin,
			GenesisLedgerFile:               "/config/pre-fork-ledger.json",
			ForkCutoverTime:                 &cutover,
			DelegationVerifyBinPathPostFork: postBin,
			GenesisLedgerFilePostFork:       "/config/post-fork-ledger.json",
		},
		Log: logging.Logger("test"),
	}
}

func TestVerifySubmissionsDualMode(t *testing.T) {
	cutover := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	preBin, preArgsFile := writeStubVerifier(t, dir, "delegation-verify-pre-fork", "pre-fork-stub")
	postBin, postArgsFile := writeStubVerifier(t, dir, "delegation-verify-post-fork", "post-fork-stub")
	appCtx := newTestAppContext(cutover, preBin, postBin)

	submissions := []Submission{
		{ID: "pre-1", SubmittedAtDate: "2026-09-02", SubmittedAt: cutover.Add(-time.Hour), Submitter: "B62pre1"},
		{ID: "pre-2", SubmittedAtDate: "2026-09-02", SubmittedAt: cutover.Add(-time.Second), Submitter: "B62pre2"},
		{ID: "at-cutover", SubmittedAtDate: "2026-09-03", SubmittedAt: cutover, Submitter: "B62boundary"},
		{ID: "post-1", SubmittedAtDate: "2026-09-03", SubmittedAt: cutover.Add(time.Hour), Submitter: "B62post1"},
	}

	verifiedSubmissions, err := appCtx.verifySubmissions(submissions)
	if err != nil {
		t.Fatalf("verifySubmissions() error = %v", err)
	}

	// each submission should be processed by exactly the right stub, and the
	// results of both runs should be merged
	wantMarkers := map[string]string{
		"pre-1":      "pre-fork-stub",
		"pre-2":      "pre-fork-stub",
		"at-cutover": "post-fork-stub",
		"post-1":     "post-fork-stub",
	}
	if len(verifiedSubmissions) != len(submissions) {
		t.Fatalf("verifySubmissions() returned %v submissions, want %v", len(verifiedSubmissions), len(submissions))
	}
	seen := make(map[string]bool)
	for _, sub := range verifiedSubmissions {
		want, known := wantMarkers[sub.ID]
		if !known {
			t.Errorf("verifySubmissions() returned unexpected submission %v", sub.ID)
			continue
		}
		if seen[sub.ID] {
			t.Errorf("verifySubmissions() returned submission %v more than once", sub.ID)
		}
		seen[sub.ID] = true
		if sub.ValidationError != want {
			t.Errorf("submission %v processed by %q, want %q", sub.ID, sub.ValidationError, want)
		}
	}

	// each stub should be invoked with its own config file
	assertStubArgs(t, preArgsFile, "stdin --config-file /config/pre-fork-ledger.json")
	assertStubArgs(t, postArgsFile, "stdin --config-file /config/post-fork-ledger.json")
}

func TestVerifySubmissionsSkipsEmptyPostForkPartition(t *testing.T) {
	cutover := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	preBin, _ := writeStubVerifier(t, dir, "delegation-verify-pre-fork", "pre-fork-stub")
	postBin, postArgsFile := writeStubVerifier(t, dir, "delegation-verify-post-fork", "post-fork-stub")
	appCtx := newTestAppContext(cutover, preBin, postBin)

	// all submissions are pre-fork, so the post-fork stub must not be invoked
	submissions := []Submission{
		{ID: "pre-1", SubmittedAtDate: "2026-09-02", SubmittedAt: cutover.Add(-time.Hour), Submitter: "B62pre1"},
	}

	verifiedSubmissions, err := appCtx.verifySubmissions(submissions)
	if err != nil {
		t.Fatalf("verifySubmissions() error = %v", err)
	}
	if len(verifiedSubmissions) != 1 || verifiedSubmissions[0].ValidationError != "pre-fork-stub" {
		t.Errorf("verifySubmissions() = %v, want single submission processed by pre-fork-stub", verifiedSubmissions)
	}
	if _, err := os.Stat(postArgsFile); !os.IsNotExist(err) {
		t.Errorf("post-fork stub was invoked for an empty partition")
	}
}

func TestVerifySubmissionsSkipsEmptyPreForkPartition(t *testing.T) {
	cutover := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	preBin, preArgsFile := writeStubVerifier(t, dir, "delegation-verify-pre-fork", "pre-fork-stub")
	postBin, _ := writeStubVerifier(t, dir, "delegation-verify-post-fork", "post-fork-stub")
	appCtx := newTestAppContext(cutover, preBin, postBin)

	// all submissions are at/after the cutover, so the pre-fork stub must not be invoked
	submissions := []Submission{
		{ID: "post-1", SubmittedAtDate: "2026-09-03", SubmittedAt: cutover.Add(time.Hour), Submitter: "B62post1"},
	}

	verifiedSubmissions, err := appCtx.verifySubmissions(submissions)
	if err != nil {
		t.Fatalf("verifySubmissions() error = %v", err)
	}
	if len(verifiedSubmissions) != 1 || verifiedSubmissions[0].ValidationError != "post-fork-stub" {
		t.Errorf("verifySubmissions() = %v, want single submission processed by post-fork-stub", verifiedSubmissions)
	}
	if _, err := os.Stat(preArgsFile); !os.IsNotExist(err) {
		t.Errorf("pre-fork stub was invoked for an empty partition")
	}
}

func assertStubArgs(t *testing.T, argsFile, want string) {
	t.Helper()
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("error reading stub args file %v: %v", argsFile, err)
	}
	if got := strings.TrimSpace(string(args)); got != want {
		t.Errorf("stub invoked with args %q, want %q", got, want)
	}
}

func TestVerifySubmissionsSingleMode(t *testing.T) {
	// With no cutover configured, verifySubmissions is one run through the
	// pre-fork binary with the pre-fork config, exactly as before.
	dir := t.TempDir()
	bin, argsFile := writeStubVerifier(t, dir, "delegation-verify", "single-mode-stub")
	appCtx := &AppContext{
		AppConfig: AppConfig{
			DelegationVerifyBinPath: bin,
			GenesisLedgerFile:       "/config/pre-fork-ledger.json",
		},
		Log: logging.Logger("test"),
	}

	submissions := []Submission{
		{ID: "sub-1", SubmittedAtDate: "2026-08-27", SubmittedAt: time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC), Submitter: "B62single"},
	}

	verifiedSubmissions, err := appCtx.verifySubmissions(submissions)
	if err != nil {
		t.Fatalf("verifySubmissions() error = %v", err)
	}
	if len(verifiedSubmissions) != 1 || verifiedSubmissions[0].ValidationError != "single-mode-stub" {
		t.Errorf("verifySubmissions() = %v, want single submission processed by single-mode-stub", verifiedSubmissions)
	}
	assertStubArgs(t, argsFile, "stdin --config-file /config/pre-fork-ledger.json")
}

// writeStubFailing creates a stub delegation-verify executable that drains
// stdin, reports a failure on stderr, and exits non-zero.
func writeStubFailing(t *testing.T, dir, name, message string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := fmt.Sprintf("#!/bin/sh\ncat > /dev/null\necho %q >&2\nexit 4\n", message)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("error writing failing stub %v: %v", name, err)
	}
	return path
}

func TestVerifySubmissionsBanksSuccessfulPartitionOnFailure(t *testing.T) {
	// A failing partition must not discard the other partition's completed
	// work: the successes are returned alongside an error naming the failure.
	cutover := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	preBin, _ := writeStubVerifier(t, dir, "delegation-verify-pre-fork", "pre-fork-stub")
	postBin := writeStubFailing(t, dir, "delegation-verify-post-fork", "post-fork boom")
	appCtx := newTestAppContext(cutover, preBin, postBin)

	submissions := []Submission{
		{ID: "pre-1", SubmittedAtDate: "2026-09-02", SubmittedAt: cutover.Add(-time.Hour), Submitter: "B62pre1"},
		{ID: "post-1", SubmittedAtDate: "2026-09-03", SubmittedAt: cutover.Add(time.Hour), Submitter: "B62post1"},
	}

	verifiedSubmissions, err := appCtx.verifySubmissions(submissions)
	if err == nil {
		t.Fatal("expected an error from the failing post-fork partition")
	}
	if !strings.Contains(err.Error(), "post-fork:") {
		t.Errorf("error should name the post-fork partition, got: %v", err)
	}
	if strings.Contains(err.Error(), "pre-fork:") {
		t.Errorf("error should not name the successful pre-fork partition, got: %v", err)
	}
	if len(verifiedSubmissions) != 1 || verifiedSubmissions[0].ID != "pre-1" || verifiedSubmissions[0].ValidationError != "pre-fork-stub" {
		t.Errorf("verifySubmissions() = %v, want the pre-fork submission processed by pre-fork-stub", verifiedSubmissions)
	}
}

func TestVerifySubmissionsReportsBothFailedPartitions(t *testing.T) {
	cutover := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	preBin := writeStubFailing(t, dir, "delegation-verify-pre-fork", "pre-fork boom")
	postBin := writeStubFailing(t, dir, "delegation-verify-post-fork", "post-fork boom")
	appCtx := newTestAppContext(cutover, preBin, postBin)

	submissions := []Submission{
		{ID: "pre-1", SubmittedAtDate: "2026-09-02", SubmittedAt: cutover.Add(-time.Hour), Submitter: "B62pre1"},
		{ID: "post-1", SubmittedAtDate: "2026-09-03", SubmittedAt: cutover.Add(time.Hour), Submitter: "B62post1"},
	}

	verifiedSubmissions, err := appCtx.verifySubmissions(submissions)
	if err == nil {
		t.Fatal("expected an error when both partitions fail")
	}
	if !strings.Contains(err.Error(), "pre-fork:") || !strings.Contains(err.Error(), "post-fork:") {
		t.Errorf("error should name both failed partitions, got: %v", err)
	}
	if len(verifiedSubmissions) != 0 {
		t.Errorf("verifySubmissions() = %v, want no submissions when both partitions fail", verifiedSubmissions)
	}
}

func TestVerifySubmissionsIsolatesMalformedRecordToItsPartition(t *testing.T) {
	// A malformed record in one partition costs that one record, per the batch
	// isolation semantics - never the rest of its partition, and never the
	// other partition.
	cutover := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	preBin, _ := writeStubVerifier(t, dir, "delegation-verify-pre-fork", "pre-fork-stub")
	postBin := writeStubPrinter(t,
		`{"submitted_at_date":"2026-09-03","submitter":"B62post1","verified":true}`,
		`{"submitted_at_date":12345}`,
	)
	appCtx := newTestAppContext(cutover, preBin, postBin)

	submissions := []Submission{
		{ID: "pre-1", SubmittedAtDate: "2026-09-02", SubmittedAt: cutover.Add(-time.Hour), Submitter: "B62pre1"},
		{ID: "post-1", SubmittedAtDate: "2026-09-03", SubmittedAt: cutover.Add(time.Hour), Submitter: "B62post1"},
		{ID: "post-2", SubmittedAtDate: "2026-09-03", SubmittedAt: cutover.Add(2 * time.Hour), Submitter: "B62post2"},
	}

	verifiedSubmissions, err := appCtx.verifySubmissions(submissions)
	if err != nil {
		t.Fatalf("verifySubmissions() error = %v", err)
	}
	if len(verifiedSubmissions) != 2 {
		t.Fatalf("verifySubmissions() returned %v submissions, want 2 (%+v)", len(verifiedSubmissions), verifiedSubmissions)
	}
	if verifiedSubmissions[0].ID != "pre-1" || verifiedSubmissions[0].ValidationError != "pre-fork-stub" {
		t.Errorf("pre-fork partition was affected: %+v", verifiedSubmissions[0])
	}
	if verifiedSubmissions[1].Submitter != "B62post1" {
		t.Errorf("surviving post-fork record = %+v, want submitter B62post1", verifiedSubmissions[1])
	}
}

// Records the verifier emits on the first pass. The sok failure carries the
// empty payload delegation_verify produces when it short-circuits on an error:
// no state_hash, parent, height or slot.
const (
	// post-fork (4.0.0 Mesa): delegation_verify's explicit check
	sokFailRecord = `{"submitted_at":"2026-09-03T01:00:00Z","submitted_at_date":"2026-09-03","submitter":"B62sok","block_hash":"hashSok","state_hash":"","verified":false,"validation_error":"proof's sok message digest does not match the sok message"}`
	// pre-fork (Berkeley / 3.5.0 stop-slot): Transaction_snark.verify's
	// internal check, reported with its own prefix and wording
	sokFailRecordPreFork = `{"submitted_at":"2026-09-03T01:00:00Z","submitted_at_date":"2026-09-03","submitter":"B62sok","block_hash":"hashSok","state_hash":"","verified":false,"validation_error":"Transaction_snark.verify: Mismatched sok_message"}`
	cleanRecord          = `{"submitted_at":"2026-09-03T02:00:00Z","submitted_at_date":"2026-09-03","submitter":"B62clean","block_hash":"hashClean","state_hash":"stateClean","verified":true}`
	otherFailRecord      = `{"submitted_at":"2026-09-03T03:00:00Z","submitted_at_date":"2026-09-03","submitter":"B62other","block_hash":"hashOther","state_hash":"","verified":false,"validation_error":"invalid block proof"}`

	// What the retry returns once the snark work is gone: the block verified on
	// its own, so the payload is complete.
	sokRetriedOKRecord = `{"submitted_at":"2026-09-03T01:00:00Z","submitted_at_date":"2026-09-03","submitter":"B62sok","block_hash":"hashSok","state_hash":"stateSok","parent":"parentSok","height":42,"slot":7,"verified":true}`
	// A retry that fails for a reason of its own.
	sokRetriedFailRecord = `{"submitted_at":"2026-09-03T01:00:00Z","submitted_at_date":"2026-09-03","submitter":"B62sok","block_hash":"hashSok","state_hash":"","verified":false,"validation_error":"invalid block proof"}`
)

// sokTestBatch is the batch matching the records above: one submission that
// fails the sok check, one clean, one failing for an unrelated reason.
func sokTestBatch(t *testing.T) string {
	t.Helper()
	submissions := []Submission{
		{SubmittedAt: time.Date(2026, 9, 3, 1, 0, 0, 0, time.UTC), Submitter: "B62sok", BlockHash: "hashSok", SnarkWork: []byte("snark")},
		{SubmittedAt: time.Date(2026, 9, 3, 2, 0, 0, 0, time.UTC), Submitter: "B62clean", BlockHash: "hashClean", SnarkWork: []byte("snark")},
		{SubmittedAt: time.Date(2026, 9, 3, 3, 0, 0, 0, time.UTC), Submitter: "B62other", BlockHash: "hashOther", SnarkWork: []byte("snark")},
	}
	batch, err := json.Marshal(submissions)
	if err != nil {
		t.Fatalf("marshaling test batch: %v", err)
	}
	return string(batch)
}

// writeStatefulStubVerifier creates a stub verifier that answers differently on
// each invocation - the first pass emits firstLines, every later one emits
// laterLines - and counts its invocations in a file so a test can tell whether
// the retry pass ran at all.
func writeStatefulStubVerifier(t *testing.T, firstLines, laterLines []string) (path string, countFile string, stdinPrefix string) {
	t.Helper()

	dir := t.TempDir()
	path = filepath.Join(dir, "delegation-verify-stateful-stub")
	countFile = filepath.Join(dir, "invocations")
	stdinPrefix = filepath.Join(dir, "stdin.")

	emit := func(lines []string) string {
		out := ""
		for _, line := range lines {
			out += "cat <<'RECORD'\n" + line + "\nRECORD\n"
		}
		return out
	}

	script := "#!/bin/sh\n" +
		"n=$(cat " + countFile + " 2>/dev/null || echo 0)\n" +
		"n=$((n + 1))\n" +
		"echo $n > " + countFile + "\n" +
		"cat > " + stdinPrefix + "$n\n" +
		"if [ \"$n\" -eq 1 ]; then\n" + emit(firstLines) +
		"else\n" + emit(laterLines) + "fi\n"

	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing stateful stub: %v", err)
	}
	return path, countFile, stdinPrefix
}

// stubStdin returns the batch the stub received on its nth invocation.
func stubStdin(t *testing.T, stdinPrefix string, n int) string {
	t.Helper()
	data, err := os.ReadFile(stdinPrefix + strconv.Itoa(n))
	if err != nil {
		t.Fatalf("reading stub stdin for invocation %d: %v", n, err)
	}
	return string(data)
}

func stubInvocations(t *testing.T, countFile string) int {
	t.Helper()
	data, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatalf("reading stub invocation count: %v", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parsing stub invocation count %q: %v", data, err)
	}
	return n
}

func TestIsSokMismatch(t *testing.T) {
	// The check moved between eras, so the waiver has to recognise both
	// spellings; anything else must stay untouched.
	testCases := []struct {
		name            string
		validationError string
		want            bool
	}{
		{
			name:            "post-fork explicit check",
			validationError: "proof's sok message digest does not match the sok message",
			want:            true,
		},
		{
			name:            "pre-fork Transaction_snark.verify check",
			validationError: "Transaction_snark.verify: Mismatched sok_message",
			want:            true,
		},
		{
			name:            "pre-fork wording with trailing context",
			validationError: "Transaction_snark.verify: Mismatched sok_message (statement 3)",
			want:            true,
		},
		{
			name:            "unrelated failure",
			validationError: "invalid block proof",
			want:            false,
		},
		{
			name:            "no failure",
			validationError: "",
			want:            false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSokMismatch(tc.validationError); got != tc.want {
				t.Errorf("isSokMismatch(%q) = %v, want %v", tc.validationError, got, tc.want)
			}
		})
	}
}

func TestRunDelegationVerifyCommandRetriesSokMismatchWithoutSnarkWork(t *testing.T) {
	// The point of the retry: a tolerated submission has to come back with a
	// real payload. Marking the short-circuited record verified would leave
	// state_hash empty, and the coordinator drops NULL state hashes before it
	// awards points - so such a submission would score zero either way.
	//
	// Both era spellings have to travel this path: the pre-fork binaries in
	// use on mainnet today report the mismatch from inside
	// Transaction_snark.verify, the post-fork ones from delegation_verify.
	testCases := []struct {
		name       string
		failRecord string
	}{
		{name: "post-fork wording", failRecord: sokFailRecord},
		{name: "pre-fork wording", failRecord: sokFailRecordPreFork},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assertSokMismatchRetried(t, tc.failRecord)
		})
	}
}

func assertSokMismatchRetried(t *testing.T, failRecord string) {
	t.Helper()

	stub, countFile, stdinPrefix := writeStatefulStubVerifier(t,
		[]string{failRecord, cleanRecord, otherFailRecord},
		[]string{sokRetriedOKRecord},
	)

	appCtx := testAppContext()
	appCtx.AppConfig.TolerateSokMismatch = true

	submissions, err := appCtx.runDelegationVerifyCommand(stub, "", sokTestBatch(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(submissions) != 3 {
		t.Fatalf("got %d submissions, want 3 (%+v)", len(submissions), submissions)
	}
	if got := stubInvocations(t, countFile); got != 2 {
		t.Errorf("verifier invoked %d time(s), want 2 (first pass and retry)", got)
	}

	// The retry must carry only the failing submission, with its snark work
	// gone - that is what makes the verifier skip the snark-work path and
	// produce a full payload.
	retryBatch := stubStdin(t, stdinPrefix, 2)
	if !strings.Contains(retryBatch, `"snark_work":null`) {
		t.Errorf("retry batch should have snark work stripped, got %s", retryBatch)
	}
	if !strings.Contains(retryBatch, "B62sok") {
		t.Errorf("retry batch should carry the failing submission, got %s", retryBatch)
	}
	if strings.Contains(retryBatch, "B62clean") || strings.Contains(retryBatch, "B62other") {
		t.Errorf("retry batch should carry only the sok-failing submission, got %s", retryBatch)
	}
	if firstBatch := stubStdin(t, stdinPrefix, 1); !strings.Contains(firstBatch, `"snark_work":"`) {
		t.Errorf("first pass should have sent the snark work, got %s", firstBatch)
	}

	retried := submissions[0]
	if !retried.Verified || retried.ValidationError != "" {
		t.Errorf("sok-mismatch record should verify on retry, got verified=%v error=%q",
			retried.Verified, retried.ValidationError)
	}
	if retried.StateHash != "stateSok" {
		t.Errorf("retried record must carry the payload the coordinator scores on, got state_hash=%q", retried.StateHash)
	}
	if retried.Parent != "parentSok" || retried.Height != 42 || retried.Slot != 7 {
		t.Errorf("retried record lost payload fields: %+v", retried)
	}

	if !submissions[1].Verified || submissions[1].ValidationError != "" || submissions[1].StateHash != "stateClean" {
		t.Errorf("clean record should be untouched, got %+v", submissions[1])
	}
	if submissions[2].Verified || submissions[2].ValidationError != "invalid block proof" {
		t.Errorf("differently-failing record should be untouched, got %+v", submissions[2])
	}
}

func TestRunDelegationVerifyCommandKeepsRetryFailure(t *testing.T) {
	// A submission that still fails without its snark work is genuinely
	// failing; the retried verdict is the accurate one and replaces the sok
	// error.
	stub, countFile, _ := writeStatefulStubVerifier(t,
		[]string{sokFailRecord, cleanRecord, otherFailRecord},
		[]string{sokRetriedFailRecord},
	)

	appCtx := testAppContext()
	appCtx.AppConfig.TolerateSokMismatch = true

	submissions, err := appCtx.runDelegationVerifyCommand(stub, "", sokTestBatch(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stubInvocations(t, countFile); got != 2 {
		t.Errorf("verifier invoked %d time(s), want 2", got)
	}
	if submissions[0].Verified || submissions[0].ValidationError != "invalid block proof" {
		t.Errorf("retried record should carry its own failure, got verified=%v error=%q",
			submissions[0].Verified, submissions[0].ValidationError)
	}
	if !submissions[1].Verified || submissions[2].Verified {
		t.Errorf("other records should be untouched, got %+v and %+v", submissions[1], submissions[2])
	}
}

func TestRunDelegationVerifyCommandKeepsSokMismatchWhenFlagOff(t *testing.T) {
	for _, tc := range []struct {
		name       string
		failRecord string
	}{
		{name: "post-fork wording", failRecord: sokFailRecord},
		{name: "pre-fork wording", failRecord: sokFailRecordPreFork},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertSokMismatchKeptWhenFlagOff(t, tc.failRecord)
		})
	}
}

func assertSokMismatchKeptWhenFlagOff(t *testing.T, failRecord string) {
	t.Helper()

	// With the flag off there is no retry at all: the verifier runs once and
	// the sok failure stands like any other.
	stub, countFile, _ := writeStatefulStubVerifier(t,
		[]string{failRecord, cleanRecord, otherFailRecord},
		[]string{sokRetriedOKRecord},
	)

	submissions, err := testAppContext().runDelegationVerifyCommand(stub, "", sokTestBatch(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stubInvocations(t, countFile); got != 1 {
		t.Errorf("verifier invoked %d time(s), want 1 (no retry pass)", got)
	}
	if len(submissions) != 3 {
		t.Fatalf("got %d submissions, want 3 (%+v)", len(submissions), submissions)
	}
	if submissions[0].Verified || !isSokMismatch(submissions[0].ValidationError) {
		t.Errorf("sok-mismatch record should keep its failure, got verified=%v error=%q",
			submissions[0].Verified, submissions[0].ValidationError)
	}
	if !submissions[1].Verified || submissions[1].ValidationError != "" {
		t.Errorf("clean record should be untouched, got %+v", submissions[1])
	}
	if submissions[2].Verified || submissions[2].ValidationError != "invalid block proof" {
		t.Errorf("differently-failing record should be untouched, got %+v", submissions[2])
	}
}

func TestRunDelegationVerifyCommandSkipsRetryWithoutSokMismatch(t *testing.T) {
	// Nothing failed the sok check, so the flag changes nothing and no second
	// invocation happens.
	stub, countFile, _ := writeStatefulStubVerifier(t,
		[]string{cleanRecord, otherFailRecord},
		[]string{sokRetriedOKRecord},
	)

	appCtx := testAppContext()
	appCtx.AppConfig.TolerateSokMismatch = true

	submissions, err := appCtx.runDelegationVerifyCommand(stub, "", sokTestBatch(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stubInvocations(t, countFile); got != 1 {
		t.Errorf("verifier invoked %d time(s), want 1 (nothing to retry)", got)
	}
	if len(submissions) != 2 {
		t.Errorf("got %d submissions, want 2 (%+v)", len(submissions), submissions)
	}
}

// Two rows a submitter produced at the same instant for the same block. This is
// not hypothetical: on the mainnet corpus 338 of 5,585 rows over six hours (6.1%)
// share a (submitted_at, submitter) pair, and in a live sink run 3 of 69
// sok-failing rows sat in such a pair. Postgres gives these rows distinct ids;
// Cassandra populates no id at all, so there they collapse onto one key.
const (
	dupFailRecord = `{"submitted_at":"2026-09-03T01:00:00Z","submitted_at_date":"2026-09-03","submitter":"B62dup","block_hash":"hashDup","state_hash":"","verified":false,"validation_error":"Transaction_snark.verify: Mismatched sok_message"}`
	dupOKRecord   = `{"submitted_at":"2026-09-03T01:00:00Z","submitted_at_date":"2026-09-03","submitter":"B62dup","block_hash":"hashDup","state_hash":"stateDup","parent":"parentDup","height":9,"slot":3,"verified":true}`

	dupFailRecordID1 = `{"id":"1","submitted_at":"2026-09-03T01:00:00Z","submitted_at_date":"2026-09-03","submitter":"B62dup","block_hash":"hashDup","state_hash":"","verified":false,"validation_error":"Transaction_snark.verify: Mismatched sok_message"}`
	dupFailRecordID2 = `{"id":"2","submitted_at":"2026-09-03T01:00:00Z","submitted_at_date":"2026-09-03","submitter":"B62dup","block_hash":"hashDup","state_hash":"","verified":false,"validation_error":"Transaction_snark.verify: Mismatched sok_message"}`
	dupOKRecordID1   = `{"id":"1","submitted_at":"2026-09-03T01:00:00Z","submitted_at_date":"2026-09-03","submitter":"B62dup","block_hash":"hashDup","state_hash":"stateDup","parent":"parentDup","height":9,"slot":3,"verified":true}`
	dupOKRecordID2   = `{"id":"2","submitted_at":"2026-09-03T01:00:00Z","submitted_at_date":"2026-09-03","submitter":"B62dup","block_hash":"hashDup","state_hash":"stateDup","parent":"parentDup","height":9,"slot":3,"verified":true}`
)

func dupTestBatch(t *testing.T, ids []string) string {
	t.Helper()
	submissions := make([]Submission, 2)
	for i := range submissions {
		submissions[i] = Submission{
			SubmittedAt: time.Date(2026, 9, 3, 1, 0, 0, 0, time.UTC),
			Submitter:   "B62dup",
			BlockHash:   "hashDup",
			SnarkWork:   []byte("snark"),
		}
		if ids != nil {
			submissions[i].ID = ids[i]
		}
	}
	batch, err := json.Marshal(submissions)
	if err != nil {
		t.Fatalf("marshaling duplicate batch: %v", err)
	}
	return string(batch)
}

func TestRunDelegationVerifyCommandRecoversEveryDuplicateKeyedRow(t *testing.T) {
	// Regression: keying the retry on (submitted_at, submitter) alone let the
	// last duplicate win, so both recovered records overwrote one slot and the
	// other row silently kept its failing verdict - while the log still counted
	// it as recovered. Every failing row must come back, in both storage shapes.
	testCases := []struct {
		name       string
		ids        []string
		failFirst  []string
		retryLater []string
	}{
		{
			name:       "postgres rows carry distinct ids",
			ids:        []string{"1", "2"},
			failFirst:  []string{dupFailRecordID1, dupFailRecordID2},
			retryLater: []string{dupOKRecordID1, dupOKRecordID2},
		},
		{
			name:       "cassandra rows carry no id and collapse onto one key",
			ids:        nil,
			failFirst:  []string{dupFailRecord, dupFailRecord},
			retryLater: []string{dupOKRecord, dupOKRecord},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stub, countFile, _ := writeStatefulStubVerifier(t, tc.failFirst, tc.retryLater)

			appCtx := testAppContext()
			appCtx.AppConfig.TolerateSokMismatch = true

			submissions, err := appCtx.runDelegationVerifyCommand(stub, "", dupTestBatch(t, tc.ids))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := stubInvocations(t, countFile); got != 2 {
				t.Fatalf("verifier invoked %d time(s), want 2 (first pass and retry)", got)
			}
			if len(submissions) != 2 {
				t.Fatalf("got %d submissions, want 2 (%+v)", len(submissions), submissions)
			}
			for i, submission := range submissions {
				if !submission.Verified || submission.ValidationError != "" {
					t.Errorf("row %d was not recovered: verified=%v error=%q",
						i, submission.Verified, submission.ValidationError)
				}
				// The payload is the whole point: a verified row with an empty
				// state_hash still scores zero.
				if submission.StateHash != "stateDup" {
					t.Errorf("row %d lost the payload the coordinator scores on: state_hash=%q",
						i, submission.StateHash)
				}
			}
		})
	}
}

func TestSubmissionKeySeparatesRowsSharingSubmitterAndTime(t *testing.T) {
	at := time.Date(2026, 9, 3, 1, 0, 0, 0, time.UTC)
	first := Submission{ID: "1", SubmittedAt: at, Submitter: "B62dup"}
	second := Submission{ID: "2", SubmittedAt: at, Submitter: "B62dup"}
	if submissionKey(first) == submissionKey(second) {
		t.Errorf("rows with distinct ids must not share a key, both are %q", submissionKey(first))
	}

	// Without an id there is nothing left to tell them apart, which is why the
	// index has to treat a key as covering a group of rows.
	noID := Submission{SubmittedAt: at, Submitter: "B62dup"}
	alsoNoID := Submission{SubmittedAt: at, Submitter: "B62dup"}
	if submissionKey(noID) != submissionKey(alsoNoID) {
		t.Errorf("id-less rows should fall back to the same key, got %q and %q",
			submissionKey(noID), submissionKey(alsoNoID))
	}
}

func TestKeyedIndexHandsOutEachRowOnce(t *testing.T) {
	index := newKeyedIndex()
	index.add("k", 4)
	index.add("k", 9)
	index.add("other", 1)

	if index.len() != 3 {
		t.Errorf("len() = %d, want 3 (it counts rows, not keys)", index.len())
	}
	if i, ok := index.take("k"); !ok || i != 4 {
		t.Errorf("first take(k) = (%d, %v), want (4, true)", i, ok)
	}
	if i, ok := index.take("k"); !ok || i != 9 {
		t.Errorf("second take(k) = (%d, %v), want (9, true)", i, ok)
	}
	// A third record carrying the same key has no row left to claim; letting it
	// through would overwrite a row that was already updated.
	if _, ok := index.take("k"); ok {
		t.Error("third take(k) should report exhaustion")
	}
	if _, ok := index.take("absent"); ok {
		t.Error("take on an unknown key should report false")
	}
}
