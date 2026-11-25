package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/stripe"
)

// The plan page of one app: whether it is a static site, a shop (Renoxium
// takes a share of its card payments) or a work app (a monthly
// subscription), and the subscription itself.
func (s *Site) registerAppPlan() {
	s.mux.HandleFunc("GET /apps/{app}/plan", s.appPlanPage)
	s.mux.HandleFunc("POST /apps/{app}/plan", s.appPlanAction)
}

func (s *Site) renderAppPlan(w http.ResponseWriter, r *http.Request, app string, status int, errText string) {
	u, _ := s.currentUser(r)
	pl, err := s.platform.AppPlan(app)
	if err != nil {
		notFound(w, err)
		return
	}
	render(w, status, "admin_plan", map[string]any{"Shell": s.shell(r, u, app, "Plan", "/apps/"+app+"/plan", false), "App": app,
		"Plan": pl, "CSRF": s.csrfToken(w, r), "Error": errText, "Ready": s.Billing.workReady(),
		"Fee": platform.ShopFeePercent, "Price": platform.WorkAppPriceAED, "Users": platform.WorkAppUsers, "ShopStaff": platform.ShopStaff,
		"PeriodEnd": formatDate(pl.WorkPeriodEnd), "SeatPrice": platform.WorkSeatPriceAED, "MaxUsers": platform.WorkAppMaxUsers,
		"Seats": s.Billing.PriceWorkSeat != "", "Monthly": platform.WorkAppPriceAED + pl.WorkSeats*platform.WorkSeatPriceAED})
}

func (s *Site) appPlanPage(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("done") == "1" && s.Billing.workReady() && s.apiLimiter.allow("stripe:"+u.ID) {
		// Back from Stripe: read the subscription now rather than wait for the webhook.
		if err := s.syncBilling(r.Context(), u.ID); err != nil {
			slog.Error("billing sync", "user", u.ID, "err", err)
		}
	}
	s.renderAppPlan(w, r, app, http.StatusOK, "")
}

// appPlanAction makes the app a shop or a work app, or starts the work app
// subscription.
func (s *Site) appPlanAction(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	pl, err := s.platform.AppPlan(app)
	if err != nil {
		notFound(w, err)
		return
	}
	switch r.PostFormValue("action") {
	case "shop":
		if pl.WorkPaid {
			s.renderAppPlan(w, r, app, http.StatusConflict, "Cancel the work app subscription first (Manage subscription), then make the app a shop.")
			return
		}
		if err := s.platform.SetAppKind(app, platform.KindShop); err != nil {
			serverError(w, err)
			return
		}
		s.platform.Note(app, u.Email+" made the app a shop")
		http.Redirect(w, r, "/apps/"+app+"/plan", http.StatusSeeOther)
	case "subscribe":
		if !s.Billing.workReady() {
			s.renderAppPlan(w, r, app, http.StatusServiceUnavailable, "Work app subscriptions are not available on this server yet.")
			return
		}
		if pl.WorkPaid {
			http.Redirect(w, r, "/apps/"+app+"/plan", http.StatusSeeOther)
			return
		}
		if !s.apiLimiter.allow("stripe:" + u.ID) {
			s.renderAppPlan(w, r, app, http.StatusTooManyRequests, "Too many attempts. Try again in a few minutes.")
			return
		}
		// The app becomes a work app once Stripe confirms the subscription.
		cus, err := s.customer(r.Context(), u)
		if err != nil {
			slog.Error("billing customer", "user", u.ID, "err", err)
			s.renderAppPlan(w, r, app, http.StatusServiceUnavailable, "Stripe is not reachable right now. Try again in a minute.")
			return
		}
		back := s.base + "/apps/" + app + "/plan"
		url, err := s.Billing.Stripe.SubscriptionCheckout(r.Context(), cus, s.Billing.PriceWorkApp, back+"?done=1", back,
			map[string]string{"rx_user": u.ID, "rx_app": app, "rx_plan": "work_app"})
		if err != nil {
			slog.Error("work app checkout", "user", u.ID, "app", app, "err", err)
			s.renderAppPlan(w, r, app, http.StatusServiceUnavailable, "Stripe could not start the payment: "+err.Error())
			return
		}
		http.Redirect(w, r, url, http.StatusSeeOther)
	case "manage":
		s.billingPortal(w, r)
	default:
		s.renderAppPlan(w, r, app, http.StatusBadRequest, "Choose shop or work app.")
	}
}

// syncWorkApps records the work app subscriptions among an account's
// Stripe subscriptions. A subscription is matched to its app by its id
// (which survives a rename) or, the first time, by the app it was bought
// for, which must still be the account's.
func (s *Site) syncWorkApps(userID string, subs []stripe.Subscription) {
	for _, sub := range subs {
		if s.Billing.PriceWorkApp == "" || sub.PriceID() != s.Billing.PriceWorkApp {
			continue
		}
		app := s.platform.AppOfWorkSubscription(sub.ID)
		if app == "" {
			app = sub.Metadata["rx_app"]
			if app == "" || s.platform.CheckOwner(userID, app) != nil {
				continue
			}
			if pl, err := s.platform.AppPlan(app); err == nil && pl.WorkPaid && pl.WorkSubscription != sub.ID {
				continue // the app is already paid by another subscription
			}
		}
		if err := s.platform.SetWorkSubscription(app, sub.ID, sub.Status, sub.PeriodEnd()); err != nil {
			slog.Error("work app subscription", "user", userID, "app", app, "err", err)
		}
		_, seats, _ := sub.Item(s.Billing.PriceWorkSeat)
		s.platform.SetWorkSeats(app, int(seats))
	}
}

// syncWorkSeats makes the app's work app subscription pay for its staff
// beyond the included ones: it adds, changes or removes the seat item.
func (s *Site) syncWorkSeats(ctx context.Context, app string) error {
	pl, err := s.platform.AppPlan(app)
	if err != nil || !pl.WorkPaid || pl.WorkSubscription == "" || pl.Included {
		return err
	}
	want := s.platform.WorkSeatsNeeded(app)
	if want == pl.WorkSeats && want == 0 {
		return nil
	}
	if s.Billing.Stripe == nil || s.Billing.PriceWorkSeat == "" {
		return errors.New("extra staff members cannot be paid for on this server")
	}
	sub, err := s.Billing.Stripe.Subscription(ctx, pl.WorkSubscription)
	if err != nil {
		return err
	}
	item, have, _ := sub.Item(s.Billing.PriceWorkSeat)
	if have != int64(want) {
		if err := s.Billing.Stripe.SetSubscriptionQuantity(ctx, sub.ID, item, s.Billing.PriceWorkSeat, int64(want)); err != nil {
			return err
		}
		slog.Info("work app seats", "app", app, "seats", want)
	}
	return s.platform.SetWorkSeats(app, want)
}
