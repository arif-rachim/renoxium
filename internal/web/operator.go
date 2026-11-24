package web

import (
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/platform"
)

func (s *Site) registerOperator() {
	s.mux.HandleFunc("GET /operator", s.operatorPage("overview"))
	s.mux.HandleFunc("GET /operator/accounts", s.operatorPage("accounts"))
	s.mux.HandleFunc("GET /operator/apps", s.operatorPage("apps"))
	s.mux.HandleFunc("POST /operator/plan", s.operatorPlan)
	s.mux.HandleFunc("POST /operator/suspend", s.operatorSuspend)
	s.mux.HandleFunc("POST /operator/abuse/{id}/resolve", s.operatorResolve)
	s.mux.HandleFunc("GET /operator/growth", s.growthPage("funnel"))
	s.mux.HandleFunc("GET /operator/growth/weeks", s.growthPage("weeks"))
	s.mux.HandleFunc("GET /operator/growth/economics", s.growthPage("economics"))
	s.mux.HandleFunc("GET /operator/growth/templates", s.growthPage("templates"))
	s.mux.HandleFunc("POST /operator/growth", s.operatorGrowth)
	s.mux.HandleFunc("GET /operator/growth.csv", s.operatorGrowthCSV)
}

// operator returns the signed-in operator. Anyone else gets 404, so the page
// does not reveal that it exists.
func (s *Site) operator(w http.ResponseWriter, r *http.Request) (accounts.User, bool) {
	u, ok := s.currentUser(r)
	if !ok || !u.Verified || !s.isOperator(u.Email) {
		http.NotFound(w, r)
		return u, false
	}
	return u, true
}

func (s *Site) isOperator(email string) bool {
	for _, o := range s.Operators {
		if strings.EqualFold(strings.TrimSpace(o), email) {
			return true
		}
	}
	return false
}

type operatorApp struct {
	platform.AppRow
	OwnerEmail string
	LiveURL    string
}

// operatorTabs are the parts of the operator's Platform section.
func operatorTabs(tab string) []shellLink {
	return []shellLink{{Label: "Overview", Href: "/operator", Current: tab == "overview"},
		{Label: "Accounts", Href: "/operator/accounts", Current: tab == "accounts"},
		{Label: "Apps", Href: "/operator/apps", Current: tab == "apps"}}
}

func (s *Site) operatorPage(tab string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { s.operatorView(w, r, tab) }
}

func (s *Site) operatorView(w http.ResponseWriter, r *http.Request, tab string) {
	op, ok := s.operator(w, r)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	total, perPlan, granted, err := s.accounts.PlanCounts()
	if err != nil {
		serverError(w, err)
		return
	}
	stats, _ := s.platform.Stats()
	// Account plans cost nothing; work apps are each their own subscription.
	workApps := s.platform.PaidWorkApps()
	mrr := platform.WorkAppPriceAED * workApps
	users, _ := s.accounts.Users(q, 100)
	appSearch := q
	if u, err := s.accounts.UserByEmail(q); err == nil {
		appSearch = u.ID // searching an owner's email lists their apps
	}
	rows, _ := s.platform.AllApps(appSearch, 100)
	emails := map[string]string{}
	apps := make([]operatorApp, 0, len(rows))
	for _, a := range rows {
		if _, seen := emails[a.Owner]; !seen {
			if u, err := s.accounts.User(a.Owner); err == nil {
				emails[a.Owner] = u.Email
			}
		}
		info, _ := s.platform.Info(a.Name)
		apps = append(apps, operatorApp{AppRow: a, OwnerEmail: emails[a.Owner], LiveURL: info.LiveURL})
	}
	reports, _ := s.platform.AbuseReports(50)
	open := 0
	for _, rep := range reports {
		if !rep.Resolved {
			open++
		}
	}
	var health any
	if s.Monitor != nil {
		health = s.Monitor.Status()
	}
	csrf := s.csrfToken(w, r)
	render(w, http.StatusOK, "operator", map[string]any{"Q": q, "Health": health, "CSRF": csrf, "Accounts": total, "PerPlan": perPlan,
		"Granted": granted, "WorkApps": workApps, "MRR": mrr, "Stats": stats, "Users": users, "Apps": apps, "Reports": reports, "OpenReports": open,
		"Tab": tab, "Tabs": operatorTabs(tab), "Here": map[string]string{"overview": "/operator", "accounts": "/operator/accounts", "apps": "/operator/apps"}[tab],
		"SearchHint": map[string]string{"accounts": "Search by email", "apps": "Search by app name or owner's email"}[tab],
		"Shell":      s.ownerShell(op, "Platform", "/operator", csrf)})
}

func (s *Site) operatorPlan(w http.ResponseWriter, r *http.Request) {
	op, ok := s.operator(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	user, plan := r.PostFormValue("user"), r.PostFormValue("plan")
	if err := s.accounts.GrantPlan(user, plan); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	platform.ForgetPlan(user)
	s.forgetGrowth()
	slog.Info("operator granted plan", "operator", op.Email, "user", user, "plan", plan)
	http.Redirect(w, r, "/operator/accounts?q="+url.QueryEscape(r.PostFormValue("q")), http.StatusSeeOther)
}

func (s *Site) operatorSuspend(w http.ResponseWriter, r *http.Request) {
	op, ok := s.operator(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	app := r.PostFormValue("app")
	var err error
	if r.PostFormValue("action") == "unsuspend" {
		err = s.platform.Unsuspend(app)
	} else {
		reason := strings.TrimSpace(r.PostFormValue("reason"))
		if reason == "" {
			reason = "suspended by Renoxium"
		}
		err = s.platform.Suspend(app, reason)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.forgetGrowth()
	slog.Warn("operator changed app", "operator", op.Email, "app", app, "action", r.PostFormValue("action"))
	http.Redirect(w, r, "/operator/apps?q="+url.QueryEscape(r.PostFormValue("q")), http.StatusSeeOther)
}

func (s *Site) operatorResolve(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.operator(w, r); !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.platform.ResolveAbuseReport(id); err != nil {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/operator", http.StatusSeeOther)
}
