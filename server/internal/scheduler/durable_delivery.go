package scheduler

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"time"
)

func smtpSendWithDeadline(address, host string, auth smtp.Auth, from string, to []string, body []byte) error {
	conn, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err = client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if auth != nil {
		if err = client.Auth(auth); err != nil {
			return err
		}
	}
	if err = client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range to {
		if err = client.Rcpt(recipient); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = writer.Write(body); err != nil {
		writer.Close()
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}
func durableDeliver(n durableNotification, key string) error {
	e := &AlertEngine{sinkSnapshot: n.Settings}
	r, ctx, msg := n.Rule, n.Context, n.Message
	errs := []error{}
	if channelWanted(r.Channels, "slack") {
		if url := e.setting("alert_slack_webhook"); url != "" {
			errs = append(errs, postJSON(url, map[string]any{"text": slackText(r.Severity, r.Name, msg, ctx.WorkflowName)}, key))
		}
	}
	if channelWanted(r.Channels, "webhook") {
		if url := e.setting("alert_webhook_url"); url != "" {
			errs = append(errs, postJSON(url, map[string]any{"event": "alert.fired", "id": fmt.Sprint(n.AlertID), "deliveryId": key, "ruleId": r.ID, "ruleName": r.Name, "severity": r.Severity, "timestamp": n.At, "workflowId": ctx.WorkflowID, "workflowName": ctx.WorkflowName, "instanceId": ctx.InstanceID, "message": msg}, key))
		}
	}
	if channelWanted(r.Channels, "email") {
		errs = append(errs, e.sendEmail(r, ctx, msg))
	}
	if channelWanted(r.Channels, "pagerduty") {
		errs = append(errs, e.sendPagerDuty(r, ctx, msg))
	}
	return errors.Join(errs...)
}
