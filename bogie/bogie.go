package bogie

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"text/template"

	"github.com/Masterminds/sprig"
	"github.com/BeenVerifiedInc/bogie/ignore"
)

type ApplicationInput struct {
	Name         string
	Env          string
	Templates    string
	Values       []string
	OverrideVars []string `yaml:"override_vars"`
}

type Bogie struct {
	RDelim            string
	LDelim            string
	EnvFile           string              `yaml:"env_file"`
	OutFile           string              `yaml:"out_file"`
	OutPath           string              `yaml:"out_path"`
	OutFormat         string              `yaml:"out_format"`
	ApplicationInputs []*ApplicationInput `yaml:"applications"`
	Rules             *ignore.Rules
	SkipImageLookup   bool   `yaml:"skip_image_lookup"`
	AppRegex          string `yaml:"app_regex"`
	FlaggedSecret     string
}

func (b *Bogie) Run() error {
	apps, err := processApplications(b)
	if err != nil {
		return err
	}

	switch b.OutFormat {
	case "dir":
		return renderTemplateToDir(b, apps)
	case "file":
		return renderTemplateToFile(b, apps)
	case "stdout":
		return renderTemplateToSTDOUT(b, apps)
	default:
		return errors.New(fmt.Sprintf("Unknown output %s", b.OutFormat))
	}
}

func (b *Bogie) InitRules() {
	if b.Rules == nil {
		b.Rules = ignore.Init()
	}
}

func runTemplate(c *context, b *Bogie, text string) (bool, io.Reader, error) {
	tmpl, err := template.New("template").
		Funcs(sprig.TxtFuncMap()).
		Funcs(initFuncs(c, b)).
		Option("missingkey=error").
		Delims(b.LDelim, b.RDelim).
		Parse(text)

	if err != nil {
		return false, nil, errors.New(fmt.Sprintf("Line %q: %v\n", text, err))
	}

	var buff bytes.Buffer
	if err := tmpl.Execute(&buff, c); err != nil {
		return false, nil, err
	}

	return hasContent(buff), &buff, nil
}

func hasContent(b bytes.Buffer) bool {
	s := bytes.TrimSpace(b.Bytes())
	return len(s) > 0
}

func renderTemplateToDir(b *Bogie, apps []*applicationOutput) error {
	for _, app := range apps {
		hasContent, buff, err := runTemplate(app.context, b, app.template)
		if err != nil {
			return fmt.Errorf("Error when writing to %s: %s\n", app.outPath, err.Error())
		}

		if hasContent {
			if err := os.MkdirAll(path.Dir(app.outPath), os.FileMode(0744)); err != nil {
				return err
			}

			f, err := openOutFile(app.outPath)
			if err != nil {
				return err
			}

			w := bufio.NewWriter(f)

			if _, err := io.Copy(w, buff); err != nil {
				return err
			}
			err = w.Flush()
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to flush output for rendering template.  %v\n", err)
			}
			err = f.Close()
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to close file. %v\n", err)
			}

		}
	}

	return nil
}

func renderTemplateToFile(b *Bogie, apps []*applicationOutput) error {
	if err := os.MkdirAll(b.OutPath, os.FileMode(0744)); err != nil {
		return err
	}

	f, err := openOutFile(fmt.Sprintf("%s/%s", b.OutPath, b.OutFile))
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	defer w.Flush()

	for _, app := range apps {
		hasContent, buff, err := runTemplate(app.context, b, app.template)
		if err != nil {
			return err
		}

		if hasContent {
			fmt.Fprint(w, "\n---\n")
			if _, err := io.Copy(w, buff); err != nil {
				return err
			}
		}
	}

	return nil
}

func renderTemplateToSTDOUT(b *Bogie, apps []*applicationOutput) error {
	w := os.Stdout
	for _, app := range apps {
		hasContent, buff, err := runTemplate(app.context, b, app.template)
		if err != nil {
			return err
		}

		if hasContent {
			fmt.Fprint(w, "\n---\n")
			if _, err := io.Copy(w, buff); err != nil {
				return err
			}
		}
	}
	return nil
}

func openOutFile(filename string) (out *os.File, err error) {
	return os.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
}
