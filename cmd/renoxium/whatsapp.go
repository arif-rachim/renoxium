package main

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

// whatsappFromEnv builds the WhatsApp sender from WHATSAPP_* variables, or
// returns nil when the three required ones are not all set.
func whatsappFromEnv() *whatsapp.CloudAPI {
	token, number, tmpl := os.Getenv("WHATSAPP_TOKEN"), os.Getenv("WHATSAPP_PHONE_NUMBER_ID"), os.Getenv("WHATSAPP_OTP_TEMPLATE")
	if token == "" || number == "" || tmpl == "" {
		return nil
	}
	lang := os.Getenv("WHATSAPP_OTP_LANGUAGE")
	if lang == "" {
		lang = os.Getenv("WHATSAPP_TEMPLATE_LANGUAGE") // older name
	}
	if lang == "" {
		lang = "en"
	}
	return &whatsapp.CloudAPI{Token: token, PhoneNumberID: number, Template: tmpl, Language: lang, Version: os.Getenv("WHATSAPP_API_VERSION"), BaseURL: os.Getenv("WHATSAPP_API_BASE"),
		NoticeTemplate: os.Getenv("WHATSAPP_NOTICE_TEMPLATE"), NoticeLanguage: os.Getenv("WHATSAPP_NOTICE_LANGUAGE")}
}

// customerTemplatesFromEnv reads WHATSAPP_CUSTOMER_TEMPLATES, e.g.
// "confirmation:rx_confirmation,reminder:rx_reminder,update:rx_update".
func customerTemplatesFromEnv() map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(os.Getenv("WHATSAPP_CUSTOMER_TEMPLATES"), ",") {
		kind, name, ok := strings.Cut(strings.TrimSpace(pair), ":")
		if !ok || name == "" {
			continue
		}
		for _, k := range platform.CustomerKinds {
			if k == strings.TrimSpace(kind) {
				out[k] = strings.TrimSpace(name)
			}
		}
	}
	return out
}

// whatsappTest sends one code to a number, to check the Meta setup without
// a live app.
func whatsappTest(args []string) int {
	if len(args) != 1 && (len(args) != 2 || args[1] != "notice" && args[1] != "customer") {
		fmt.Fprintln(os.Stderr, "usage: renoxium whatsapp-test +971501234567 [notice|customer]")
		return 2
	}
	phone, ok := whatsapp.Normalize(args[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "%q is not a phone number with a country code\n", args[0])
		return 2
	}
	wa := whatsappFromEnv()
	if wa == nil {
		fmt.Fprintln(os.Stderr, "WHATSAPP_TOKEN, WHATSAPP_PHONE_NUMBER_ID and WHATSAPP_OTP_TEMPLATE must all be set")
		return 2
	}
	version := wa.Version
	if version == "" {
		version = "v23.0 (default)"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if len(args) == 2 && args[1] == "customer" {
		tmpl := customerTemplatesFromEnv()
		if len(tmpl) == 0 {
			fmt.Fprintln(os.Stderr, "WHATSAPP_CUSTOMER_TEMPLATES is not set")
			return 2
		}
		failed := 0
		for _, kind := range platform.CustomerKinds {
			if tmpl[kind] == "" {
				continue
			}
			params := []string{"Test Shop", "Booking #1", "Tue 2 Dec, 10:00", "Haircut with Sara"}
			if kind != "reminder" {
				params = []string{"Test Shop", "Booking #1", "Haircut with Sara on Tue 2 Dec, 10:00"}
			}
			fmt.Printf("sending %s template %q to %s\n", kind, tmpl[kind], phone)
			if _, err := wa.SendTemplate(ctx, phone, whatsapp.Message{Template: tmpl[kind], Language: os.Getenv("WHATSAPP_CUSTOMER_LANGUAGE"),
				Body: params, URLParam: "test", StopPayload: "rx-stop:test"}); err != nil {
				fmt.Fprintln(os.Stderr, "FAILED:", err)
				failed++
			}
		}
		if failed > 0 {
			return 1
		}
		fmt.Println("OK: Meta accepted the customer templates.")
		return 0
	}
	if len(args) == 2 {
		if wa.NoticeTemplate == "" {
			fmt.Fprintln(os.Stderr, "WHATSAPP_NOTICE_TEMPLATE is not set")
			return 2
		}
		fmt.Printf("sending notice template %q from phone number ID %s, API %s, to %s\n", wa.NoticeTemplate, wa.PhoneNumberID, version, phone)
		if err := wa.SendNotice(ctx, phone, "test-shop", "New order #1: AED 100.00"); err != nil {
			fmt.Fprintln(os.Stderr, "FAILED:", err)
			return 1
		}
		fmt.Println("OK: Meta accepted the notice.")
		return 0
	}
	fmt.Printf("sending template %q, language %q, from phone number ID %s, API %s, to %s\n", wa.Template, wa.Language, wa.PhoneNumberID, version, phone)
	if err := wa.SendCode(ctx, phone, "123456"); err != nil {
		fmt.Fprintln(os.Stderr, "FAILED:", err)
		return 1
	}
	fmt.Println("OK: Meta accepted the message. The phone should receive code 123456 shortly.")
	return 0
}

// buildVersion is the git commit the binary was built from, as recorded by
// the Go toolchain when building inside a git checkout.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "unknown"
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if dirty {
		rev += "-modified"
	}
	return rev
}
