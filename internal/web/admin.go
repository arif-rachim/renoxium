package web

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/files"
	"github.com/arif-rachim/renoxium/internal/platform"
)

func (s *Site) registerAdmin() {
	s.mux.HandleFunc("GET /apps/{app}", s.adminApp)
	s.mux.HandleFunc("GET /apps/{app}/t/{table}", s.adminList)
	s.mux.HandleFunc("GET /apps/{app}/t/{table}/new", s.adminForm)
	s.mux.HandleFunc("POST /apps/{app}/t/{table}/new", s.adminSave)
	s.mux.HandleFunc("GET /apps/{app}/t/{table}/{id}", s.adminForm)
	s.mux.HandleFunc("POST /apps/{app}/t/{table}/{id}", s.adminSave)
	s.mux.HandleFunc("POST /apps/{app}/t/{table}/{id}/delete", s.adminDelete)
	s.mux.HandleFunc("GET /apps/{app}/visitors", s.adminVisitors)
	s.mux.HandleFunc("POST /apps/{app}/visitors/{id}/delete", s.adminDeleteVisitor)
	s.mux.HandleFunc("GET /apps/{app}/preview", s.adminPreview)
	s.mux.HandleFunc("GET /apps/{app}/publish", s.publishForm)
	s.mux.HandleFunc("POST /apps/{app}/publish", s.publishConfirm)
}

func (s *Site) publishLink(app string, version int) string {
	return fmt.Sprintf("%s/apps/%s/publish?version=%d", s.base, app, version)
}

// publishForm asks the owner to confirm a publish their agent asked for.
func (s *Site) publishForm(w http.ResponseWriter, r *http.Request) {
	_, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	version, err := strconv.Atoi(r.URL.Query().Get("version"))
	if err != nil || version < 1 {
		notFound(w, err)
		return
	}
	if _, _, err := s.platform.Definition(app, version); err != nil {
		notFound(w, err)
		return
	}
	info, err := s.platform.Info(app)
	if err != nil {
		serverError(w, err)
		return
	}
	render(w, http.StatusOK, "publish", map[string]any{"Info": info, "Version": version, "CSRF": s.csrfToken(w, r), "Rollback": version < info.LiveVersion})
}

func (s *Site) publishConfirm(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	version, err := strconv.Atoi(r.PostFormValue("version"))
	if err != nil || version < 1 {
		notFound(w, err)
		return
	}
	res, err := s.platform.Publish(app, version)
	if err != nil {
		render(w, http.StatusConflict, "error", map[string]any{"Message": "Could not publish: " + err.Error()})
		return
	}
	s.platform.Note(app, fmt.Sprintf("%s published version %d", u.Email, version))
	render(w, http.StatusOK, "notice", map[string]any{"Title": "Published", "Message": fmt.Sprintf("Version %d is live at %s.", res.LiveVersion, res.LiveURL), "Link": res.LiveURL})
}

// adminPreview sends the owner to their app's private preview with a
// short-lived link that unlocks it in this browser.
func (s *Site) adminPreview(w http.ResponseWriter, r *http.Request) {
	if _, app, ok := s.member(w, r); ok {
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.Redirect(w, r, s.platform.PreviewLink(app), http.StatusSeeOther)
	}
}

// owner returns the signed-in user if they own the app in the path. It has
// already answered the request when ok is false.
func (s *Site) owner(w http.ResponseWriter, r *http.Request) (u accounts.User, app string, ok bool) {
	app = r.PathValue("app")
	u, signedIn := s.currentUser(r)
	if !signedIn {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return u, app, false
	}
	if s.platform.CheckOwner(u.ID, app) != nil {
		render(w, http.StatusNotFound, "error", map[string]any{"Message": "There is no app called " + app + " in your account."})
		return u, app, false
	}
	return u, app, true
}

// member returns the signed-in user if they own the app in the path or are
// on its staff with a confirmed email (staff are matched by email, so an
// unconfirmed address must not get access). It has already answered the
// request when ok is false.
func (s *Site) member(w http.ResponseWriter, r *http.Request) (u accounts.User, app string, ok bool) {
	app = r.PathValue("app")
	u, signedIn := s.currentUser(r)
	if !signedIn {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return u, app, false
	}
	if s.platform.CheckOwner(u.ID, app) == nil || (u.Verified && s.platform.IsStaff(app, u.Email)) {
		return u, app, true
	}
	render(w, http.StatusNotFound, "error", map[string]any{"Message": "There is no app called " + app + " in your account."})
	return u, app, false
}

func (s *Site) isOwnerOf(u accounts.User, app string) bool {
	return s.platform.CheckOwner(u.ID, app) == nil
}

func (s *Site) adminApp(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.member(w, r)
	if !ok {
		return
	}
	info, err := s.platform.Info(app)
	if err != nil {
		serverError(w, err)
		return
	}
	tables, err := s.platform.Tables(app)
	if err != nil {
		serverError(w, err)
		return
	}
	type tableRow struct {
		Name  string
		Label string
		Rows  int
	}
	var rows []tableRow
	for _, t := range tables {
		_, total, err := s.platform.AdminList(r.Context(), app, t.Name, "", 1)
		if err != nil {
			serverError(w, err)
			return
		}
		rows = append(rows, tableRow{Name: t.Name, Label: tableLabel(t), Rows: total})
	}
	problems, _ := s.platform.Logs(app, applog.Filter{Stage: platform.Live, Level: "error", Limit: 10})
	_, visitors, _ := s.platform.Visitors(r.Context(), app, "", 1)
	currency, sales, _ := s.platform.Sales(r.Context(), app)
	render(w, http.StatusOK, "admin_app", map[string]any{"Info": info, "Tables": rows, "Problems": problems, "Visitors": visitors, "Sales": sales, "Currency": currency,
		"IsOwner": s.isOwnerOf(u, app), "MCPURL": s.platform.LiveMCPURL(app)})
}

type column struct {
	Name, Label string
}

func (s *Site) adminList(w http.ResponseWriter, r *http.Request) {
	_, app, ok := s.member(w, r)
	if !ok {
		return
	}
	table := r.PathValue("table")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	q := r.URL.Query().Get("q")
	t, err := s.table(app, table)
	if err != nil {
		notFound(w, err)
		return
	}
	rows, total, err := s.platform.AdminList(r.Context(), app, table, q, page)
	if err != nil {
		serverError(w, err)
		return
	}
	cols := []column{{Name: "id", Label: "#"}}
	for i, c := range t.Columns {
		if i == 5 {
			break
		}
		cols = append(cols, column{Name: c.Name, Label: columnLabel(c)})
	}
	cols = append(cols, column{Name: "created_at", Label: "Created"})
	types := map[string]string{}
	for _, c := range t.Columns {
		types[c.Name] = c.Type
	}
	var cells [][]string
	var ids []int64
	for _, row := range rows {
		var line []string
		for _, c := range cols {
			line = append(line, short(display(row[c.Name], types[c.Name])))
		}
		cells = append(cells, line)
		ids = append(ids, row["id"].(int64))
	}
	pages := int(math.Ceil(float64(total) / platform.AdminPageSize))
	render(w, http.StatusOK, "admin_list", map[string]any{
		"App": app, "Table": table, "Label": tableLabel(t), "Columns": cols, "Cells": cells, "IDs": ids,
		"Total": total, "Query": q, "Page": page, "Pages": pages, "Prev": page - 1, "Next": page + 1,
	})
}

type field struct {
	Name, Label, Help, Type, Input, Value, Error string
	Options                                      []string
	Required, Checked                            bool
}

func (s *Site) adminForm(w http.ResponseWriter, r *http.Request) {
	_, app, ok := s.member(w, r)
	if !ok {
		return
	}
	table := r.PathValue("table")
	t, err := s.table(app, table)
	if err != nil {
		notFound(w, err)
		return
	}
	var row map[string]any
	var id int64
	if raw := r.PathValue("id"); raw != "" {
		if id, err = strconv.ParseInt(raw, 10, 64); err != nil {
			notFound(w, err)
			return
		}
		if row, err = s.platform.AdminGet(r.Context(), app, table, id); err != nil {
			notFound(w, err)
			return
		}
	}
	values := map[string]string{}
	for _, c := range t.Columns {
		values[c.Name] = formValue(row[c.Name], c.Type)
	}
	s.renderForm(w, r, http.StatusOK, app, t, id, row, values, nil, r.URL.Query().Get("saved") == "1")
}

func (s *Site) renderForm(w http.ResponseWriter, r *http.Request, status int, app string, t appdef.Table, id int64, row map[string]any,
	values map[string]string, errs platform.FieldErrors, saved bool) {
	var fields []field
	hasImage := false
	for _, c := range t.Columns {
		f := field{Name: c.Name, Label: columnLabel(c), Help: c.Help, Type: c.Type, Value: values[c.Name], Required: c.Required, Error: errs[c.Name]}
		switch {
		case len(c.Options) > 0:
			f.Input, f.Options = "select", c.Options
			if f.Value != "" && !slices.Contains(c.Options, f.Value) {
				f.Options = append([]string{f.Value}, c.Options...)
			}
		}
		switch c.Type {
		case "boolean":
			f.Input, f.Checked = "checkbox", f.Value == "1"
		case "integer", "real":
			f.Input = "number"
		case "datetime":
			f.Input = "datetime-local"
		case "json":
			f.Input = "textarea"
		case "image":
			f.Input = "image"
			hasImage = true
		default:
			if f.Input == "select" {
				break
			}
			f.Input = "text"
			if len(f.Value) > 80 || strings.ContainsAny(f.Value, "\n") || longText(c.Name) {
				f.Input = "textarea"
			}
		}
		fields = append(fields, f)
	}
	var other string
	for k, v := range errs {
		if !hasColumn(t, k) {
			other = label(k) + ": " + v
		}
	}
	liveURL := ""
	if info, err := s.platform.Info(app); err == nil {
		liveURL = info.LiveURL
	}
	created := ""
	if row != nil {
		created = display(row["created_at"], "datetime")
	}
	render(w, status, "admin_form", map[string]any{
		"App": app, "Table": t.Name, "Label": tableLabel(t), "ID": id, "Fields": fields, "Created": created,
		"CSRF": s.csrfToken(w, r), "Saved": saved, "Error": other, "HasErrors": len(errs) > 0,
		"HasImage": hasImage, "LiveURL": liveURL,
	})
}

func (s *Site) adminSave(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.member(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, files.MaxSize+1<<20)
	if ct := r.Header.Get("Content-Type"); strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				render(w, http.StatusRequestEntityTooLarge, "error", map[string]any{"Message": files.ErrTooLarge.Error() + ". Go back and choose a smaller image."})
				return
			}
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		defer r.MultipartForm.RemoveAll()
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	table := r.PathValue("table")
	t, err := s.table(app, table)
	if err != nil {
		notFound(w, err)
		return
	}
	var id int64
	if raw := r.PathValue("id"); raw != "" {
		if id, err = strconv.ParseInt(raw, 10, 64); err != nil {
			notFound(w, err)
			return
		}
	}
	values := map[string]string{}
	uploadErrs := platform.FieldErrors{}
	for _, c := range t.Columns {
		values[c.Name] = r.PostFormValue(c.Name)
		if c.Type != "image" {
			continue
		}
		if r.PostFormValue(c.Name+"__remove") == "1" {
			values[c.Name] = ""
		}
		if file, _, err := r.FormFile(c.Name + "__upload"); err == nil {
			info, err := s.platform.AddFile(app, file)
			file.Close()
			if err != nil {
				uploadErrs[c.Name] = err.Error()
				continue
			}
			values[c.Name] = info.URL
		}
	}
	var newID int64
	if len(uploadErrs) > 0 {
		err = uploadErrs
	} else {
		newID, err = s.platform.AdminSave(r.Context(), app, table, id, values, u.Email)
	}
	var ferr platform.FieldErrors
	switch {
	case errors.As(err, &ferr):
		var row map[string]any
		if id != 0 {
			row, _ = s.platform.AdminGet(r.Context(), app, table, id)
		}
		s.renderForm(w, r, http.StatusUnprocessableEntity, app, t, id, row, values, ferr, false)
	case errors.Is(err, platform.ErrNotFound):
		notFound(w, err)
	case err != nil:
		serverError(w, err)
	default:
		http.Redirect(w, r, fmt.Sprintf("/apps/%s/t/%s/%d?saved=1", app, table, newID), http.StatusSeeOther)
	}
}

func (s *Site) adminDelete(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.member(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	table := r.PathValue("table")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		notFound(w, err)
		return
	}
	if err := s.platform.AdminDelete(r.Context(), app, table, id, u.Email); err != nil {
		if errors.Is(err, platform.ErrNotFound) {
			notFound(w, err)
			return
		}
		render(w, http.StatusConflict, "error", map[string]any{"Message": "Could not delete: " + err.Error()})
		return
	}
	http.Redirect(w, r, "/apps/"+app+"/t/"+table, http.StatusSeeOther)
}

func (s *Site) adminVisitors(w http.ResponseWriter, r *http.Request) {
	_, app, ok := s.member(w, r)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	q := r.URL.Query().Get("q")
	list, total, err := s.platform.Visitors(r.Context(), app, q, page)
	if err != nil {
		serverError(w, err)
		return
	}
	render(w, http.StatusOK, "admin_visitors", map[string]any{
		"App": app, "Visitors": list, "Total": total, "Query": q, "Page": page,
		"Pages": int(math.Ceil(float64(total) / platform.AdminPageSize)), "Prev": page - 1, "Next": page + 1, "CSRF": s.csrfToken(w, r),
	})
}

func (s *Site) adminDeleteVisitor(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		notFound(w, err)
		return
	}
	if err := s.platform.DeleteVisitor(r.Context(), app, id, u.Email); err != nil {
		notFound(w, err)
		return
	}
	http.Redirect(w, r, "/apps/"+app+"/visitors", http.StatusSeeOther)
}

func (s *Site) table(app, name string) (appdef.Table, error) {
	tables, err := s.platform.Tables(app)
	if err != nil {
		return appdef.Table{}, err
	}
	for _, t := range tables {
		if t.Name == name {
			return t, nil
		}
	}
	return appdef.Table{}, platform.ErrNotFound
}

func hasColumn(t appdef.Table, name string) bool {
	for _, c := range t.Columns {
		if c.Name == name {
			return true
		}
	}
	return false
}

func notFound(w http.ResponseWriter, err error) {
	render(w, http.StatusNotFound, "error", map[string]any{"Message": "Not found."})
}

func serverError(w http.ResponseWriter, err error) {
	render(w, http.StatusInternalServerError, "error", map[string]any{"Message": "Something went wrong. Try again in a moment."})
}

// label turns price_aed into "Price aed".
func tableLabel(t appdef.Table) string {
	if t.Label != "" {
		return t.Label
	}
	return label(t.Name)
}

func columnLabel(c appdef.Column) string {
	if c.Label != "" {
		return c.Label
	}
	return label(c.Name)
}

func label(name string) string {
	s := strings.ReplaceAll(name, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func longText(name string) bool {
	for _, w := range []string{"description", "notes", "note", "address", "body", "content", "message", "details"} {
		if strings.Contains(name, w) {
			return true
		}
	}
	return false
}

// display formats a stored value for reading.
func display(v any, typ string) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		if typ == "image" && x != "" {
			return "Image"
		}
		return x
	case int64:
		if typ == "boolean" {
			if x != 0 {
				return "Yes"
			}
			return "No"
		}
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

// formValue formats a stored value for an input field.
func formValue(v any, typ string) string {
	switch typ {
	case "image":
		s, _ := v.(string)
		return s
	case "boolean":
		if x, ok := v.(int64); ok && x != 0 {
			return "1"
		}
		return ""
	case "datetime":
		s, _ := v.(string)
		if len(s) >= 16 {
			return strings.Replace(s[:16], " ", "T", 1)
		}
		return s
	}
	return display(v, typ)
}

func short(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}
