//go:build mage

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	cueCmd "cuelang.org/go/cmd/cue/cmd"
	"github.com/google/renameio"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/samber/lo"
	"github.com/zclconf/go-cty/cty"
)

type LocalVars struct {
	ToDisplayName map[string]string `json:"to_display_name" cue:"to_display_name"`
	ToFixed       map[string]string `json:"to_fixed" cue:"to_fixed"`
	ToShort       map[string]string `json:"to_short" cue:"to_short"`
}

var logger slog.Logger = *slog.New(slog.NewTextHandler(os.Stderr, nil))

func parseLocalsFromHCL(contents []byte, filename string) (*LocalVars, error) {
	parser := hclparse.NewParser()

	// Parse the HCL file
	f, diags := parser.ParseHCL(contents, filename)
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to parse HCL: %s", diags.Error())
	}

	// Get the root body
	content, _, diags := f.Body.PartialContent(&hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{
				Type: "locals",
			},
		},
	})

	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to get content: %s", diags.Error())
	}

	result := &LocalVars{
		ToDisplayName: make(map[string]string),
		ToFixed:       make(map[string]string),
		ToShort:       make(map[string]string),
	}

	// Iterate through all locals blocks
	for _, block := range content.Blocks {
		attrs, diags := block.Body.JustAttributes()
		if diags.HasErrors() {
			return nil, fmt.Errorf("failed to get attributes: %s", diags.Error())
		}

		// Process each attribute in the locals block
		for name, attr := range attrs {
			// Try to evaluate the expression to get its value
			val, diags := attr.Expr.Value(&hcl.EvalContext{})
			if diags.HasErrors() {
				continue // Skip if we can't evaluate
			}

			// Check if it's a map and process accordingly
			if val.Type().IsObjectType() {
				switch name {
				case "to_display_name":
					result.ToDisplayName = unmarshalObjectValue(val)
				case "to_fixed":
					result.ToFixed = unmarshalObjectValue(val)
				case "to_short":
					result.ToShort = unmarshalObjectValue(val)
				}
			}
		}
	}

	return result, nil
}

func unmarshalObjectValue(val cty.Value) map[string]string {
	result := make(map[string]string)

	if !val.Type().IsObjectType() {
		return result
	}

	for it := val.ElementIterator(); it.Next(); {
		key, value := it.Element()
		if key.Type() == cty.String && value.Type() == cty.String {
			result[key.AsString()] = value.AsString()
		}
	}

	return result
}

const (
	outfile       = "static.cue"
	licenseNotice = `
// Copyright 2020-2024 [name of copyright owner]
// Derived from terraform-aws-utils (https://github.com/cloudposse/terraform-aws-utils)
// Original work Copyright 2020-2024 Cloud Posse, LLC
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This file is generated automatically.
// To update, run %s
`
)

func Update() error {
	resp := lo.Must(http.Get(`https://raw.githubusercontent.com/cloudposse/terraform-aws-utils/refs/heads/main/main.tf`))
	ctx := context.Background()
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		panic(fmt.Sprintf("failed to fetch main.tf: %s", resp.Status))
	}
	body := lo.Must(io.ReadAll(resp.Body))
	// Example usage
	locals, err := parseLocalsFromHCL(body, "config.hcl")
	if err != nil {
		return fmt.Errorf("Failed to parse hcl: %w", err)
	}

	r, w := io.Pipe()
	file := lo.Must(renameio.TempFile("", outfile))
	defer file.Close()
	cmd := lo.Must(cueCmd.New([]string{
		"--verbose",
		"import",
		"--package", "static",
		"--outfile", "-", // output
		"json:", "-", // input
	}))

	fmt.Fprintf(file, strings.TrimSpace(licenseNotice)+"\n", "`mage update`")

	cmd.SetIn(r)
	cmd.SetOut(file)
	cmd.SetErr(os.Stderr)

	ch := make(chan error, 2)
	go func() {
		defer w.Close()
		logger.Info("Inside goroutine")
		encoder := json.NewEncoder(w)
		logger.Info("Starting encode")
		if err := encoder.Encode(locals); err != nil {
			ch <- fmt.Errorf("Failed to encode JSON: %w", err)
		}
		close(ch)
	}()
	logger.Info("Executing command")
	if err := cmd.ExecuteContext(ctx); err != nil {
		return fmt.Errorf("Command failed: %w", err)
	}
	logger.Info("Command executed")

	if <-ch != nil {
		return fmt.Errorf("JSON encoding failed: %w", err)
	}
	file.CloseAtomicallyReplace()

	return nil
}
