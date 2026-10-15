package main

import (
	"context"
	"fmt"
	"os"
	"time"

	logging "github.com/ipfs/go-log/v2"
)

func main() {
	logging.SetupLogging(logging.Config{
		Format: logging.JSONOutput,
		Stderr: false,
		Stdout: true,
		Level:  logging.LevelDebug,
		File:   "",
	})
	log := logging.Logger("Submission Updater")
	startTime, endTime := parseArgs(log)

	appCfg := LoadEnv(log)
	ctx := context.Background()

	log.Info("Submission Updater started...")
	log.Info("Using SUBMISSION_STORAGE: ", appCfg.SubmissionStorage)
	log.Infof("Using DELEGATION_VERIFY_BIN_PATH: %v", appCfg.DelegationVerifyBinPath)
	if appCfg.TolerateSokMismatch {
		log.Warn("TOLERATE_SOK_MISMATCH enabled: submissions failing only the snark-work sok-digest check are re-verified with the snark work stripped, so the block proof must still verify on its own; see MinaProtocol/mina#19299")
	}

	appCtx, err := NewAppContext(ctx, appCfg, log)
	if err != nil {
		log.Fatalf("Error creating context: %v", err)
	}

	log.Infof("S3 session initialized")
	log.Infof("Selecting submissions in range: (%v, %v)", startTime.Format("2006-01-02 15:04:05.0-0700"), endTime.Format("2006-01-02 15:04:05.0-0700"))

	submissions, err := appCtx.selectRange(startTime, endTime)
	if err != nil {
		log.Fatalf("Error selecting range: %v", err)
	}
	numberOfReturnedSubmissions := len(submissions)
	log.Infof("Number of returned submissions: %v", numberOfReturnedSubmissions)

	if numberOfReturnedSubmissions == 0 {
		log.Info("No submissions to verify")
		os.Exit(0)
	} else {
		log.Info("Adding missing blocks from S3...")
		submissions = appCtx.addMissingBlocksFromS3(ctx, submissions, appCfg)

		log.Info("Running delegation verification...")
		verifiedSubmissions, verifyErr := appCtx.verifySubmissions(submissions)

		// Bank whatever verified before acting on any verification error:
		// updates are idempotent, and a scheduled runner advances its window
		// rather than retrying it, so results not written now would never be
		// written at all.
		if len(verifiedSubmissions) > 0 {
			// Update the submissions
			err = appCtx.updateSubmissions(verifiedSubmissions)
			if err != nil {
				log.Fatalf("Error updating submissions: %v", err)
			}

			for _, sub := range verifiedSubmissions {
				if sub.ValidationError != "" || !sub.Verified {
					log.Infof("[INVALID] Submitter: %s, Block hash: %s, Submitted at: %s, Validation error: %s, Verified: %v",
						sub.Submitter, sub.BlockHash, sub.SubmittedAt, sub.ValidationError, sub.Verified)
				}
			}
		}

		// Still exit non-zero on a verification failure so alerting fires,
		// but only after any successful partition's results are written.
		if verifyErr != nil {
			log.Fatalf("Error running command: %v", verifyErr)
		}
	}
}

func parseArgs(log logging.EventLogger) (startTime time.Time, endTime time.Time) {
	if len(os.Args) < 3 {
		fmt.Println("Usage: <program> <start date> <end date>")
		os.Exit(1)
	}

	startDate := os.Args[1]
	endDate := os.Args[2]

	var err error
	startTime, err = time.Parse("2006-01-02 15:04:05.0-0700", startDate)
	if err != nil {
		log.Fatalf("Error parsing start date:", err)
	}

	endTime, err = time.Parse("2006-01-02 15:04:05.0-0700", endDate)
	if err != nil {
		log.Fatalf("Error parsing end date:", err)
	}

	return startTime, endTime
}
