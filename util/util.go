package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"

	"github.com/PagerDuty/go-pagerduty"
)

const (
	IncidentDedupKeyBlockTimeout = "block_timeout"
	IncidentDedupKeyRelayError   = "relay_error"
)

var alerter Alerter

var pagerDutyAuthToken = ""

type Alerter struct {
	BotId    string
	ChatId   string
	SlackApp string
}

func InitAlert(cfg *AlertConfig) {
	alerter = Alerter{
		SlackApp: cfg.SlackApp,
	}
}

// SendTelegramMessage sends message to telegram group
func SendTelegramMessage(msg string) {
	if alerter.BotId == "" || alerter.ChatId == "" || msg == "" {
		return
	}

	endPoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", alerter.BotId)
	formData := url.Values{
		"chat_id":    {alerter.ChatId},
		"parse_mode": {"html"},
		"text":       {msg},
	}
	Logger.Infof("send tg message, bot_id=%s, chat_id=%s, msg=%s", alerter.BotId, alerter.ChatId, msg)
	res, err := http.PostForm(endPoint, formData)
	if err != nil {
		Logger.Errorf("send telegram message error, bot_id=%s, chat_id=%s, msg=%s, err=%s", alerter.BotId, alerter.ChatId, msg, err.Error())
		return
	}

	bodyBytes, err := ioutil.ReadAll(res.Body)
	defer res.Body.Close()
	if err != nil {
		Logger.Errorf("read http response error, err=%s", err.Error())
		return
	}
	Logger.Infof("tg response: %s", string(bodyBytes))
}

func SendSlackMessage(msg string) {
	if alerter.SlackApp == "" || msg == "" {
		return
	}
	Logger.Infof("send slack message, app=%s, msg=%s", alerter.SlackApp, alerter.ChatId, msg)

	type SlackReq struct {
		Text string `json:"text"`
	}
	slackReq := SlackReq{
		Text: msg,
	}
	reqBytes, _ := json.Marshal(slackReq)

	req, err := http.NewRequest("POST", fmt.Sprintf("https://hooks.slack.com/services/%s", alerter.SlackApp), bytes.NewReader(reqBytes))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := HttpClient.Do(req)
	if err != nil {
		Logger.Errorf("send slack message error, err=%s", err.Error())
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		Logger.Errorf("read slack response error, err=%s", err.Error())
		return
	}
	Logger.Infof("send slack message success, res=%s", string(body))
}

func SendPagerDutyAlert(detail string, dedupKey string) {
	if pagerDutyAuthToken == "" {
		return
	}

	event := pagerduty.V2Event{
		RoutingKey: pagerDutyAuthToken,
		Action:     "trigger",
		DedupKey:   dedupKey,
		Payload: &pagerduty.V2Payload{
			Summary:   "oracle relayer error detected, please contact Zhenxing (13041017167), Haoyang (15618304832), or Fudong (13732255759)",
			Source:    "sdk",
			Severity:  "error",
			Component: "oracle_relayer",
			Group:     "dex",
			Class:     "oracle_relayer",
			Details:   detail,
		},
	}
	_, err := pagerduty.ManageEvent(event)
	if err != nil {
		Logger.Errorf("send pager duty alert error, err=%s", err.Error())
	}
}

// QuotedStrToIntWithBitSize convert a QuoteStr ""6""  to int 6
func QuotedStrToIntWithBitSize(str string, bitSize int) (int64, error) {
	s, err := strconv.Unquote(str)
	if err != nil {
		return 0, err
	}
	num, err := strconv.ParseInt(s, 10, bitSize)
	if err != nil {
		return 0, err
	}
	return num, nil
}
