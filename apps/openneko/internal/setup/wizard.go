package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/open-neko/neko/apps/openneko/internal/ui"
)

// Config carries everything the onboarding flow needs. Headless callers set the
// override fields and Headless=true; interactive callers leave them empty.
type Config struct {
	Mode    string // demo | prod | dev — picks the data-source default
	BaseURL string // web app URL, for the browser handoff message

	Headless bool // run from flags without prompting

	AdminPassword    string
	Provider         string
	ProviderKey      string
	Model            string
	DataURL          string
	ResearchProvider string
	ResearchKey      string
	NoResearch       bool
}

// Outcome reports how onboarding ended so the caller can print the right
// next-step message.
type Outcome struct {
	Configured bool // ran through Finish in the terminal
	Skipped    bool // deferred to the browser (chosen, or no TTY + no flags)
	// PasswordSet is the new DB password when this run rotated it (empty
	// otherwise). The caller persists it to the host config and reconnects the
	// agent gateway, which baked the old password at bring-up.
	PasswordSet string
}

// errBrowser is returned by any step when the operator aborts to the browser.
var errBrowser = errors.New("browser handoff")

var forbiddenPasswords = map[string]bool{"secret": true, "password": true, "postgres": true}

const minPasswordLen = 8

// Run executes onboarding against the web app. Headless runs from Config
// fields; interactive prompts step by step and can bail to the browser at any
// point (partial progress persists server-side, so resuming in the browser just
// continues).
func Run(ctx context.Context, c *Client, out io.Writer, cfg Config) (Outcome, error) {
	if cfg.Headless {
		return runHeadless(ctx, c, cfg)
	}
	return runInteractive(ctx, c, out, cfg)
}

// ----- interactive -----

func runInteractive(ctx context.Context, c *Client, out io.Writer, cfg Config) (Outcome, error) {
	choice := "terminal"
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("OpenNeko is up — how do you want to configure it?").
			Options(
				huh.NewOption("Configure here in the terminal", "terminal"),
				huh.NewOption("Finish in the browser", "browser"),
			).
			Value(&choice),
	))
	if err := runForm(form); err != nil {
		if errors.Is(err, errBrowser) {
			return browserHandoff(out, cfg), nil
		}
		return Outcome{}, err
	}
	if choice == "browser" {
		return browserHandoff(out, cfg), nil
	}

	pw, err := stepsInteractive(ctx, c, out, cfg)
	if errors.Is(err, errBrowser) {
		// A password set before bailing still needs the gateway reconnect, so
		// carry it onto the browser-handoff outcome.
		o := browserHandoff(out, cfg)
		o.PasswordSet = pw
		return o, nil
	}
	if err != nil {
		// A rotation that succeeded before the failing step must still reach
		// the caller — the host config needs the new password either way.
		return Outcome{PasswordSet: pw}, err
	}
	return Outcome{Configured: true, PasswordSet: pw}, nil
}

// stepsInteractive runs the terminal onboarding steps, returning the new DB
// password when it rotated one this run (empty otherwise) alongside any error.
func stepsInteractive(ctx context.Context, c *Client, out io.Writer, cfg Config) (passwordSet string, err error) {
	changed, err := c.PasswordChanged(ctx)
	if err != nil {
		return passwordSet, err
	}

	total := 3
	if !changed {
		total = 4
	}
	step := 0
	next := func(title, desc string) {
		step++
		ui.StepHeader(out, step, total, title, desc)
	}

	// 1. Admin DB password (only when still the bootstrap default).
	if !changed {
		next("Database password", "Pick a password only you know — you won't enter it again.")
		var pw, confirm string
		form := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("New password").EchoMode(huh.EchoModePassword).Value(&pw).Validate(validatePassword),
			huh.NewInput().Title("Confirm password").EchoMode(huh.EchoModePassword).Value(&confirm).
				Validate(func(s string) error {
					if s != pw {
						return errors.New("passwords don't match")
					}
					return nil
				}),
		))
		if err := runForm(form); err != nil {
			return passwordSet, err
		}
		if err := ui.Spin("Setting database password", func() error { return c.ChangePassword(ctx, pw) }); err != nil {
			return passwordSet, err
		}
		passwordSet = pw
		ui.Success(out, "password set")
	}

	// 2. Data source (re-prompts until the connectivity test passes).
	ds, err := c.GetDataSource(ctx)
	if err != nil {
		return passwordSet, err
	}
	next("Connect your data", "GraphJin base URL — OpenNeko adds the GraphQL & MCP paths.")
	root := defaultDataURL(cfg, ds)
	for {
		form := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("GraphJin URL").Value(&root).Validate(nonEmpty),
		))
		if err := runForm(form); err != nil {
			return passwordSet, err
		}
		gql, mcp := DeriveEndpoints(root)
		var mcpOK bool
		if err := ui.Spin("Testing connection", func() error {
			var e error
			mcpOK, e = c.TestDataSource(ctx, gql, mcp)
			return e
		}); err != nil {
			ui.Failure(out, "%v — try again", err)
			continue
		}
		if err := ui.Spin("Saving data source", func() error { return c.SaveDataSource(ctx, gql, mcp, "primary") }); err != nil {
			ui.Failure(out, "%v — try again", err)
			continue
		}
		if mcpOK {
			ui.Success(out, "data source saved")
		} else {
			ui.Success(out, "data source saved (MCP unreachable — fine for the agent path)")
		}
		break
	}

	// 3. Agent model provider (re-prompts until the key validates).
	agent, err := c.GetAgent(ctx)
	if err != nil {
		return passwordSet, err
	}
	prov, err := c.GetProvider(ctx)
	if err != nil {
		return passwordSet, err
	}
	next("Agent & model provider", "The model that runs your metric queries.")
	provider := prov.Primary.Provider
	if err := runForm(huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Model provider").Options(providerOptions(prov.Options.Primary)...).Value(&provider),
	))); err != nil {
		return passwordSet, err
	}
	capJobs := agent.Agent.GlobalCap
	if capJobs == 0 {
		capJobs = agent.Defaults.GlobalCap
	}
	for {
		model, config, secrets, err := providerForm(modelDefault(prov.Defaults.Primary, provider, prov.Primary), prov.Fields.Primary[provider])
		if err != nil {
			return passwordSet, err
		}
		draft := ProviderDraft{Scope: "primary", Provider: provider, Model: model, Enabled: true, Config: config, Secrets: secrets}
		if err := ui.Spin("Validating provider key", func() error { return c.TestProvider(ctx, draft) }); err != nil {
			ui.Failure(out, "%v — try again", err)
			continue
		}
		if err := ui.Spin("Saving agent + provider", func() error {
			if e := c.SaveAgent(ctx, capJobs); e != nil {
				return e
			}
			return c.SaveProvider(ctx, draft)
		}); err != nil {
			return passwordSet, err
		}
		ui.Success(out, "agent + provider saved")
		break
	}

	// 4. Research (optional).
	next("Industry research", "Optional — pull industry context during onboarding.")
	enable := false
	if err := runForm(huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Enable industry research?").Affirmative("Yes").Negative("No, skip").Value(&enable),
	))); err != nil {
		return passwordSet, err
	}
	if enable {
		for {
			rprovider := researchDefault(prov.Options.Research)
			if err := runForm(huh.NewForm(huh.NewGroup(
				huh.NewSelect[string]().Title("Research provider").
					Options(providerOptions(filterOut(prov.Options.Research, "disabled"))...).Value(&rprovider),
			))); err != nil {
				return passwordSet, err
			}
			model, config, secrets, err := providerForm(prov.Defaults.Research[rprovider], prov.Fields.Research[rprovider])
			if err != nil {
				return passwordSet, err
			}
			rdraft := ProviderDraft{Scope: "research", Provider: rprovider, Model: model, Enabled: true, Config: config, Secrets: secrets}
			if err := ui.Spin("Validating research key", func() error { return c.TestProvider(ctx, rdraft) }); err != nil {
				ui.Failure(out, "%v — try again", err)
				continue
			}
			if err := ui.Spin("Enabling research", func() error { return c.SaveProvider(ctx, rdraft) }); err != nil {
				return passwordSet, err
			}
			ui.Success(out, "research enabled")
			break
		}
	} else if err := ui.Spin("Saving", func() error { return c.SaveProvider(ctx, disabledResearch()) }); err != nil {
		return passwordSet, err
	}

	// 5. Finish.
	return passwordSet, ui.Spin("Finishing setup", func() error { return c.Finish(ctx) })
}

// runForm runs a huh form with the shared theme. A user abort (ctrl-c / esc) is
// mapped to errBrowser so the caller can fall back to the browser.
func runForm(f *huh.Form) error {
	err := f.WithTheme(ui.Theme()).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return errBrowser
	}
	return err
}

// providerForm prompts for the model plus the provider's declared fields,
// routing secret-kind fields through a masked input. Returns the model and the
// config/secrets maps for a ProviderDraft.
func providerForm(modelDef string, fields []Field) (model string, config, secrets map[string]string, err error) {
	model = modelDef
	config = map[string]string{}
	secrets = map[string]string{}
	formFields := []huh.Field{
		huh.NewInput().Title("Model").Value(&model).Validate(nonEmpty),
	}
	ptrs := make([]*string, len(fields))
	for i, fld := range fields {
		p := new(string)
		ptrs[i] = p
		in := huh.NewInput().Title(fld.Label).Value(p)
		if fld.Kind == "secret" {
			in = in.EchoMode(huh.EchoModePassword)
		}
		if fld.Placeholder != "" {
			in = in.Placeholder(fld.Placeholder)
		}
		if fld.Required {
			in = in.Validate(nonEmpty)
		}
		formFields = append(formFields, in)
	}
	if err = runForm(huh.NewForm(huh.NewGroup(formFields...))); err != nil {
		return "", nil, nil, err
	}
	for i, fld := range fields {
		if fld.Kind == "secret" {
			secrets[fld.Key] = *ptrs[i]
		} else {
			config[fld.Key] = *ptrs[i]
		}
	}
	return model, config, secrets, nil
}

func providerOptions(in []ProviderOption) []huh.Option[string] {
	out := make([]huh.Option[string], len(in))
	for i, o := range in {
		out[i] = huh.NewOption(optionLabel(o.Value, o.Description), o.Value)
	}
	return out
}

func optionLabel(value, desc string) string {
	if desc == "" {
		return value
	}
	return value + " — " + desc
}

func nonEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("required")
	}
	return nil
}

// ----- headless -----

func runHeadless(ctx context.Context, c *Client, cfg Config) (Outcome, error) {
	passwordSet := ""
	changed, err := c.PasswordChanged(ctx)
	if err != nil {
		return Outcome{PasswordSet: passwordSet}, err
	}
	if !changed {
		if cfg.AdminPassword == "" {
			return Outcome{PasswordSet: passwordSet}, errors.New("setup: --admin-password is required (database still has the bootstrap default)")
		}
		if err := validatePassword(cfg.AdminPassword); err != nil {
			return Outcome{PasswordSet: passwordSet}, err
		}
		if err := c.ChangePassword(ctx, cfg.AdminPassword); err != nil {
			return Outcome{PasswordSet: passwordSet}, err
		}
		passwordSet = cfg.AdminPassword
	}

	ds, err := c.GetDataSource(ctx)
	if err != nil {
		return Outcome{PasswordSet: passwordSet}, err
	}
	gql, mcp := DeriveEndpoints(defaultDataURL(cfg, ds))
	if _, err := c.TestDataSource(ctx, gql, mcp); err != nil {
		return Outcome{PasswordSet: passwordSet}, fmt.Errorf("data source test failed: %w", err)
	}
	if err := c.SaveDataSource(ctx, gql, mcp, "primary"); err != nil {
		return Outcome{PasswordSet: passwordSet}, err
	}

	agent, err := c.GetAgent(ctx)
	if err != nil {
		return Outcome{PasswordSet: passwordSet}, err
	}
	prov, err := c.GetProvider(ctx)
	if err != nil {
		return Outcome{PasswordSet: passwordSet}, err
	}
	provider := cfg.Provider
	if provider == "" {
		return Outcome{PasswordSet: passwordSet}, errors.New("setup: --provider is required")
	}
	if cfg.ProviderKey == "" {
		return Outcome{PasswordSet: passwordSet}, errors.New("setup: --provider-key is required")
	}
	model := cfg.Model
	if model == "" {
		model = modelDefault(prov.Defaults.Primary, provider, prov.Primary)
	}
	secretKey := secretFieldKey(prov.Fields.Primary[provider])
	capJobs := agent.Agent.GlobalCap
	if capJobs == 0 {
		capJobs = agent.Defaults.GlobalCap
	}
	draft := ProviderDraft{
		Scope: "primary", Provider: provider, Model: model, Enabled: true,
		Config: map[string]string{}, Secrets: map[string]string{secretKey: cfg.ProviderKey},
	}
	if err := c.TestProvider(ctx, draft); err != nil {
		return Outcome{}, fmt.Errorf("provider key test failed: %w", err)
	}
	if err := c.SaveAgent(ctx, capJobs); err != nil {
		return Outcome{PasswordSet: passwordSet}, err
	}
	if err := c.SaveProvider(ctx, draft); err != nil {
		return Outcome{PasswordSet: passwordSet}, err
	}

	if !cfg.NoResearch && cfg.ResearchProvider != "" && cfg.ResearchKey != "" {
		rmodel := prov.Defaults.Research[cfg.ResearchProvider]
		rkey := secretFieldKey(prov.Fields.Research[cfg.ResearchProvider])
		rdraft := ProviderDraft{
			Scope: "research", Provider: cfg.ResearchProvider, Model: rmodel, Enabled: true,
			Config: map[string]string{}, Secrets: map[string]string{rkey: cfg.ResearchKey},
		}
		if err := c.TestProvider(ctx, rdraft); err != nil {
			return Outcome{}, fmt.Errorf("research key test failed: %w", err)
		}
		if err := c.SaveProvider(ctx, rdraft); err != nil {
			return Outcome{PasswordSet: passwordSet}, err
		}
	} else if err := c.SaveProvider(ctx, disabledResearch()); err != nil {
		return Outcome{PasswordSet: passwordSet}, err
	}

	if err := c.Finish(ctx); err != nil {
		return Outcome{PasswordSet: passwordSet}, err
	}
	return Outcome{Configured: true, PasswordSet: passwordSet}, nil
}

// ----- shared helpers -----

func browserHandoff(out io.Writer, cfg Config) Outcome {
	ui.Info(out, "Finish setup in your browser:")
	fmt.Fprintln(out, "  "+cfg.BaseURL)
	return Outcome{Skipped: true}
}

func defaultDataURL(cfg Config, ds *DataSource) string {
	if ds != nil && ds.Source == "org" && ds.GraphqlURL != "" {
		return deriveRoot(ds.GraphqlURL)
	}
	if cfg.DataURL != "" {
		return cfg.DataURL
	}
	if cfg.Mode == "demo" {
		return "http://openneko-serve:8080"
	}
	if cfg.Mode == "dev" {
		return "http://localhost:8080"
	}
	return "http://openneko-serve:8080"
}

func modelDefault(defaults map[string]string, provider string, current ProviderConfig) string {
	if d := defaults[provider]; d != "" {
		return d
	}
	if current.Provider == provider && current.Model != "" {
		return current.Model
	}
	return ""
}

func researchDefault(opts []ProviderOption) string {
	for _, o := range opts {
		if o.Value != "disabled" {
			return o.Value
		}
	}
	return ""
}

func disabledResearch() ProviderDraft {
	return ProviderDraft{Scope: "research", Provider: "disabled", Model: "", Enabled: false, Config: map[string]string{}, Secrets: map[string]string{}}
}

func filterOut(opts []ProviderOption, drop string) []ProviderOption {
	out := make([]ProviderOption, 0, len(opts))
	for _, o := range opts {
		if o.Value != drop {
			out = append(out, o)
		}
	}
	return out
}

// secretFieldKey returns the key of the first secret-kind field, defaulting to
// "apiKey" when the provider declares none explicitly.
func secretFieldKey(fields []Field) string {
	for _, f := range fields {
		if f.Kind == "secret" {
			return f.Key
		}
	}
	return "apiKey"
}

func validatePassword(pw string) error {
	if len(pw) < minPasswordLen {
		return fmt.Errorf("password must be at least %d characters", minPasswordLen)
	}
	if forbiddenPasswords[strings.ToLower(pw)] {
		return errors.New("that password is too common; pick something else")
	}
	return nil
}
