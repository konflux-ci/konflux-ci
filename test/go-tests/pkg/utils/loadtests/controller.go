package loadtests

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
)

// User represents a user in the list
type User struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Token    string `json:"token"`
	SSOURL   string `json:"ssourl"`
	APIURL   string `json:"apiurl"`
	Verified bool   `json:"verified"`
}

func LoadStageUsers(filePath string) ([]User, error) {
	jsonData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	// Parse JSON into an array of User objects
	var users []User
	err = json.Unmarshal(jsonData, &users)
	if err != nil {
		return nil, err
	}
	return users, nil
}

func SelectUsers(userList []User, numberOfUsers, threadCount, maxUsers int) ([]User, error) {
	// Check if the requested number of users exceeds the maximum
	if numberOfUsers*threadCount > maxUsers {
		return nil, fmt.Errorf("%s", "requested number of users exceeds maximum")
	}

	// Create a new list to store the selected users
	selectedUsers := make([]User, 0)

	// Iterate through the list and select Z users
	selectedCount := numberOfUsers * threadCount
	for i := 0; i < selectedCount; i++ {
		if i >= len(userList) {
			break // Stop if all users are selected
		}
		selectedUsers = append(selectedUsers, userList[i])
	}
	return selectedUsers, nil
}

// Indentify CI and get unique Job Name
func GetJobName(name string) string {

	var jobName string
	if name != "" {
		jobName = name
	} else {
		if utils.CheckIfEnvironmentExists("CI") {
			if utils.CheckIfEnvironmentExists("GITHUB_ACTIONS") {
				jobName = utils.GetEnv("GITHUB_RUN_ID", "")
			} else if utils.CheckIfEnvironmentExists("PROW_JOB_ID") && utils.CheckIfEnvironmentExists("BUILD_ID") {
				jobName = utils.GetEnv("BUILD_ID", "")
			} else {
				jobName = time.Now().String()
			}
		} else {
			jobName = time.Now().String()
		}
	}
	return jobName
}

// Parse and check url
func UrlCheck(urlString string) bool {
	_, err := url.ParseRequestURI(urlString)

	if err != nil {
		return false
	} else {
		return true
	}
}
