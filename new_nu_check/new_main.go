package main

import (
	"errors"
	"github.com/gin-gonic/gin"
	"io/ioutil"
	"log"
	"net/http"
	"net/smtp"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	subscriptions    = make(map[string]chan bool)
	subscriptionsMux sync.Mutex
)

func main() {
	router := gin.Default()

	router.POST("/start-course-check", startCourseCheckHandler)
	router.POST("/stop-course-check", stopCourseCheckHandler)

	err := router.Run(":8080")
	if err != nil {
		log.Fatal("Failed to run server: ", err)
	}
}

func startCourseCheckHandler(c *gin.Context) {
	email := c.Query("email")
	crn := c.Query("CRN")
	if email == "" || crn == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email and CRN are required"})
		return
	}

	subscriptionsMux.Lock()
	if _, exists := subscriptions[email]; exists {
		subscriptionsMux.Unlock()
		c.JSON(http.StatusBadRequest, gin.H{"error": "A check is already running for this email"})
		return
	}

	stopChan := make(chan bool)
	subscriptions[email] = stopChan
	subscriptionsMux.Unlock()

	go checkCourseAvailability(email, crn, stopChan)
	c.JSON(http.StatusOK, gin.H{"message": "Course availability check started for " + email})
}

func stopCourseCheckHandler(c *gin.Context) {
	email := c.Query("email")
	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email is required"})
		return
	}

	subscriptionsMux.Lock()
	if stopChan, exists := subscriptions[email]; exists {
		stopChan <- true
		close(stopChan)
		delete(subscriptions, email)
		subscriptionsMux.Unlock()
		c.JSON(http.StatusOK, gin.H{"message": "Course check stopped for " + email})
		return
	}
	subscriptionsMux.Unlock()

	c.JSON(http.StatusBadRequest, gin.H{"error": "No active check found for this email"})
}

func checkCourseAvailability(email string, crn string, stopChan chan bool) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			apiUrl := "https://nubanner.neu.edu/StudentRegistrationSsb/ssb/searchResults/getEnrollmentInfo"
			form := url.Values{}
			form.Add("term", "202430")
			form.Add("courseReferenceNumber", crn)

			req, err := http.NewRequest("POST", apiUrl, strings.NewReader(form.Encode()))
			if err != nil {
				log.Println("Error creating request: ", err)
				return
			}
			req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				log.Println("Error making request: ", err)
				return
			}
			defer resp.Body.Close()

			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				log.Println("Error reading response body: ", err)
				return
			}

			availableSeats, err := parseAvailableSeats(string(body))
			if err != nil {
				log.Println("Error parsing available seats: ", err)
				return
			}

			if availableSeats > 0 {
				sendEmailNotification(email, availableSeats, crn)
			}
		case <-stopChan:
			log.Println("Stopping course check for", email)
			return
		}
	}
}

func parseAvailableSeats(htmlStr string) (int, error) {
	re := regexp.MustCompile(`Enrollment Seats Available:</span> <span dir="ltr"> (-?\d+) </span>`)
	matches := re.FindStringSubmatch(htmlStr)
	if len(matches) < 2 {
		return 0, errors.New("could not find available seats in HTML")
	}

	return strconv.Atoi(matches[1])
}

func sendEmailNotification(email string, availableSeats int, crn string) {
	from := "jianznucheck@gmail.com"
	password := "kpuqoqjynyiplkqh"
	to := []string{email}

	smtpHost := "smtp.gmail.com"
	smtpPort := "587"

	message := []byte("Subject: Course Slot Available\r\n\r\nA slot is available. There are " + strconv.Itoa(availableSeats) + " seats available for you subscribe course: " + crn)

	auth := smtp.PlainAuth("", from, password, smtpHost)
	err := smtp.SendMail(smtpHost+":"+smtpPort, auth, from, to, message)
	if err != nil {
		log.Println("Error sending email: ", err)
	} else {
		log.Println("Send Notification to", email, "Successfully")
	}
}
