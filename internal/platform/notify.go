package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arif-rachim/renoxium/internal/email"
)

type mailInput struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

func (in *mailInput) clean(needTo bool) error {
	in.Subject = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(in.Subject, "\r", " "), "\n", " "))
	switch {
	case in.Subject == "" || utf8.RuneCountInString(in.Subject) > 150:
		return errors.New("subject is required (at most 150 characters)")
	case strings.TrimSpace(in.Text) == "" || len(in.Text) > 10000:
		return errors.New("text is required (at most 10000 characters)")
	}
	if needTo {
		addr, ok := normalizeEmail(in.To)
		if !ok {
			return fmt.Errorf("to %q is not a valid email address", in.To)
		}
		in.To = addr
	}
	return nil
}

// emailSend answers email.send({to, subject, text}): a plain-text email
// from the app to one address, such as an order confirmation. Live sends it
// (under the daily limits); preview writes it to the app log.
func (p *Platform) emailSend(ctx context.Context, st *stage, arg any) (any, error) {
	var in mailInput
	if err := decodeArg(arg, &in); err != nil {
		return nil, errors.New("email.send expects {to, subject, text}")
	}
	if err := in.clean(true); err != nil {
		return nil, err
	}
	if !p.allow("mail-to:"+st.app+":"+in.To, 10, 24*time.Hour) {
		return nil, errors.New("too many emails to this address today")
	}
	p.sendEmail(ctx, st, email.Message{To: in.To, Subject: in.Subject, Text: in.Text, Tag: "app_email"})
	return map[string]any{"sent": st.name == Live}, nil
}

// notifyOwner answers notify.owner({subject, text}): an email to the app
// owner's confirmed address, such as "New order #12".
func (p *Platform) notifyOwner(ctx context.Context, st *stage, arg any) (any, error) {
	var in mailInput
	if err := decodeArg(arg, &in); err != nil {
		return nil, errors.New("notify.owner expects {subject, text}")
	}
	if err := in.clean(false); err != nil {
		return nil, err
	}
	p.mailOwner(ctx, st, in.Subject, in.Text)
	p.whatsappOwner(st, in.Subject)
	return map[string]any{"sent": st.name == Live}, nil
}

// mailOwner emails the app owner from Renoxium. Preview logs it instead.
func (p *Platform) mailOwner(ctx context.Context, st *stage, subject, text string) {
	if st.name == Preview {
		p.event(st, "email", "info", "", fmt.Sprintf("not sent from preview: to the owner, %q\n%s", subject, text))
		return
	}
	if p.cfg.OwnerEmail == nil || p.cfg.Mailer == nil {
		return
	}
	addr, ok := p.cfg.OwnerEmail(p.ownerOf(st.app))
	if !ok {
		p.event(st, "email", "warn", "", "owner not emailed (no confirmed email address): "+subject)
		return
	}
	if !p.allow("owner-mail:"+st.app, 200, 24*time.Hour) {
		p.event(st, "email", "error", "", "owner not emailed: more than 200 notifications today: "+subject)
		return
	}
	link := ""
	if p.cfg.SiteURL != "" {
		link = "\n\nManage " + st.app + ": " + p.cfg.SiteURL + "/apps/" + st.app + "\n"
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := p.cfg.Mailer.Send(ctx, email.Message{To: addr, Subject: "[" + st.app + "] " + subject, Text: text + link, Tag: "owner_notice", FromName: "Renoxium"}); err != nil {
		p.event(st, "email", "error", "", "owner email failed: "+err.Error())
	}
}
