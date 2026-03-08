package slack

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/slack-go/slack"
	v1 "k8s.io/test-infra/prow/apis/prowjobs/v1"
	"sigs.k8s.io/yaml"
)

func ReportIssue(msg string, errLevel ErrorSeverityLevel) error {
	api := slack.New(os.Getenv(constants.SLACK_BOT_TOKEN_ENV))
	msg = fmt.Sprintf("%s\nError message: ```\n%s\n```", getMessageHeader(errLevel), msg)

	jobID := os.Getenv("PROW_JOB_ID")
	if jobID != "" {
		msg += fmt.Sprintf("\n<%s|*View logs*>", getProwJobURL(jobID))
	}

	_, _, err := api.PostMessage(
		constants.SlackCIReportsChannelID,
		slack.MsgOptionText(msg, false),
		slack.MsgOptionAsUser(true),
	)
	return err
}

func getMessageHeader(errLevel ErrorSeverityLevel) string {
	headerMsg := "*E2E job alert*"
	return fmt.Sprintf("%s %s %s", alertEmojiType[errLevel], headerMsg, alertEmojiType[errLevel])
}

func getProwJobURL(jobID string) string {
	r, err := http.Get(fmt.Sprintf("https://prow.ci.openshift.org/prowjob?prowjob=%s", jobID))
	errTemplate := "failed to get prow job URL:"
	if err != nil {
		return fmt.Sprintf("%s %s", errTemplate, err)
	}
	if r.StatusCode > 299 {
		return fmt.Sprintf("%s got response status code %v", errTemplate, r.StatusCode)
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Sprintf("%s %s", errTemplate, err)
	}
	var pj v1.ProwJob
	err = yaml.Unmarshal(body, &pj)
	if err != nil {
		return fmt.Sprintf("%s %s", errTemplate, err)
	}
	return pj.Status.URL
}
