package types

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/gptscript-ai/gptscript/pkg/system"
	"golang.org/x/exp/maps"
)

const (
	DaemonPrefix  = "#!sys.daemon"
	OpenAPIPrefix = "#!sys.openapi"
	CommandPrefix = "#!"
)

type ErrToolNotFound struct {
	ToolName string
}

func NewErrToolNotFound(toolName string) *ErrToolNotFound {
	return &ErrToolNotFound{
		ToolName: toolName,
	}
}

func (e *ErrToolNotFound) Error() string {
	return fmt.Sprintf("tool not found: %s", e.ToolName)
}

type ToolSet map[string]Tool

type Program struct {
	Name        string  `json:"name,omitempty"`
	EntryToolID string  `json:"entryToolId,omitempty"`
	ToolSet     ToolSet `json:"toolSet,omitempty"`
}

func (p Program) GetContextToolIDs(toolID string) (result []string, _ error) {
	seen := map[string]struct{}{}
	tool := p.ToolSet[toolID]

	subToolIDs, err := tool.GetToolIDsFromNames(tool.Tools)
	if err != nil {
		return nil, err
	}

	for _, subToolID := range subToolIDs {
		subTool := p.ToolSet[subToolID]
		exportContextToolIDs, err := subTool.GetToolIDsFromNames(subTool.ExportContext)
		if err != nil {
			return nil, err
		}
		for _, exportContextToolID := range exportContextToolIDs {
			if _, ok := seen[exportContextToolID]; !ok {
				seen[exportContextToolID] = struct{}{}
				result = append(result, exportContextToolID)
			}
		}
	}

	contextToolIDs, err := p.ToolSet[toolID].GetToolIDsFromNames(p.ToolSet[toolID].Context)
	if err != nil {
		return nil, err
	}

	for _, contextToolID := range contextToolIDs {
		if _, ok := seen[contextToolID]; !ok {
			seen[contextToolID] = struct{}{}
			result = append(result, contextToolID)
		}
	}

	return
}

func (p Program) GetCompletionTools() (result []CompletionTool, err error) {
	return Tool{
		Parameters: Parameters{
			Tools: []string{"main"},
		},
		ToolMapping: map[string]string{
			"main": p.EntryToolID,
		},
	}.GetCompletionTools(p)
}

func (p Program) TopLevelTools() (result []Tool) {
	for _, tool := range p.ToolSet[p.EntryToolID].LocalTools {
		result = append(result, p.ToolSet[tool])
	}
	return
}

func (p Program) SetBlocking() Program {
	tool := p.ToolSet[p.EntryToolID]
	tool.Blocking = true
	tools := maps.Clone(p.ToolSet)
	tools[p.EntryToolID] = tool
	p.ToolSet = tools
	return p
}

type BuiltinFunc func(ctx context.Context, env []string, input string) (string, error)

type Parameters struct {
	Name           string           `json:"name,omitempty"`
	Description    string           `json:"description,omitempty"`
	MaxTokens      int              `json:"maxTokens,omitempty"`
	ModelName      string           `json:"modelName,omitempty"`
	ModelProvider  bool             `json:"modelProvider,omitempty"`
	JSONResponse   bool             `json:"jsonResponse,omitempty"`
	Temperature    *float32         `json:"temperature,omitempty"`
	Cache          *bool            `json:"cache,omitempty"`
	InternalPrompt *bool            `json:"internalPrompt"`
	Arguments      *openapi3.Schema `json:"arguments,omitempty"`
	Tools          []string         `json:"tools,omitempty"`
	Context        []string         `json:"context,omitempty"`
	ExportContext  []string         `json:"exportContext,omitempty"`
	Export         []string         `json:"export,omitempty"`
	Credentials    []string         `json:"credentials,omitempty"`
	Blocking       bool             `json:"-"`
}

type Tool struct {
	Parameters   `json:",inline"`
	Instructions string `json:"instructions,omitempty"`

	ID          string            `json:"id,omitempty"`
	ToolMapping map[string]string `json:"toolMapping,omitempty"`
	LocalTools  map[string]string `json:"localTools,omitempty"`
	BuiltinFunc BuiltinFunc       `json:"-"`
	Source      ToolSource        `json:"source,omitempty"`
	WorkingDir  string            `json:"workingDir,omitempty"`
}

func (t Tool) GetToolIDsFromNames(names []string) (result []string, _ error) {
	for _, toolName := range names {
		toolID, ok := t.ToolMapping[toolName]
		if !ok {
			return nil, NewErrToolNotFound(toolName)
		}
		result = append(result, toolID)
	}
	return
}

func (t Tool) String() string {
	buf := &strings.Builder{}
	if t.Parameters.Name != "" {
		_, _ = fmt.Fprintf(buf, "Name: %s\n", t.Parameters.Name)
	}
	if t.Parameters.Description != "" {
		_, _ = fmt.Fprintf(buf, "Description: %s\n", t.Parameters.Description)
	}
	if len(t.Parameters.Tools) != 0 {
		_, _ = fmt.Fprintf(buf, "Tools: %s\n", strings.Join(t.Parameters.Tools, ", "))
	}
	if len(t.Parameters.Export) != 0 {
		_, _ = fmt.Fprintf(buf, "Export: %s\n", strings.Join(t.Parameters.Export, ", "))
	}
	if len(t.Parameters.ExportContext) != 0 {
		_, _ = fmt.Fprintf(buf, "Export Context: %s\n", strings.Join(t.Parameters.ExportContext, ", "))
	}
	if len(t.Parameters.Context) != 0 {
		_, _ = fmt.Fprintf(buf, "Context: %s\n", strings.Join(t.Parameters.Context, ", "))
	}
	if t.Parameters.MaxTokens != 0 {
		_, _ = fmt.Fprintf(buf, "Max Tokens: %d\n", t.Parameters.MaxTokens)
	}
	if t.Parameters.ModelName != "" {
		_, _ = fmt.Fprintf(buf, "Model: %s\n", t.Parameters.ModelName)
	}
	if t.Parameters.ModelProvider {
		_, _ = fmt.Fprintf(buf, "Model Provider: true\n")
	}
	if t.Parameters.JSONResponse {
		_, _ = fmt.Fprintln(buf, "JSON Response: true")
	}
	if t.Parameters.Cache != nil && !*t.Parameters.Cache {
		_, _ = fmt.Fprintln(buf, "Cache: false")
	}
	if t.Parameters.Temperature != nil {
		_, _ = fmt.Fprintf(buf, "Temperature: %f", *t.Parameters.Temperature)
	}
	if t.Parameters.Arguments != nil {
		var keys []string
		for k := range t.Parameters.Arguments.Properties {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			prop := t.Parameters.Arguments.Properties[key]
			_, _ = fmt.Fprintf(buf, "Args: %s: %s\n", key, prop.Value.Description)
		}
	}
	if t.Parameters.InternalPrompt != nil {
		_, _ = fmt.Fprintf(buf, "Internal Prompt: %v\n", *t.Parameters.InternalPrompt)
	}
	if t.Instructions != "" && t.BuiltinFunc == nil {
		_, _ = fmt.Fprintln(buf)
		_, _ = fmt.Fprintln(buf, t.Instructions)
	}
	if len(t.Parameters.Credentials) > 0 {
		_, _ = fmt.Fprintf(buf, "Credentials: %s\n", strings.Join(t.Parameters.Credentials, ", "))
	}

	return buf.String()
}

func (t Tool) GetCompletionTools(prg Program) (result []CompletionTool, err error) {
	toolNames := map[string]struct{}{}

	for _, subToolName := range t.Parameters.Tools {
		result, err = appendTool(result, prg, t, subToolName, toolNames)
		if err != nil {
			return nil, err
		}
	}

	for _, subToolName := range t.Parameters.Context {
		result, err = appendExports(result, prg, t, subToolName, toolNames)
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}

func getTool(prg Program, parent Tool, name string) (Tool, error) {
	toolID, ok := parent.ToolMapping[name]
	if !ok {
		return Tool{}, &ErrToolNotFound{
			ToolName: name,
		}
	}
	tool, ok := prg.ToolSet[toolID]
	if !ok {
		return Tool{}, &ErrToolNotFound{
			ToolName: name,
		}
	}
	return tool, nil
}

func appendExports(completionTools []CompletionTool, prg Program, parentTool Tool, subToolName string, toolNames map[string]struct{}) ([]CompletionTool, error) {
	subTool, err := getTool(prg, parentTool, subToolName)
	if err != nil {
		return nil, err
	}

	for _, export := range subTool.Export {
		completionTools, err = appendTool(completionTools, prg, subTool, export, toolNames)
		if err != nil {
			return nil, err
		}
	}

	return completionTools, nil
}

func appendTool(completionTools []CompletionTool, prg Program, parentTool Tool, subToolName string, toolNames map[string]struct{}) ([]CompletionTool, error) {
	subTool, err := getTool(prg, parentTool, subToolName)
	if err != nil {
		return nil, err
	}

	args := subTool.Parameters.Arguments
	if args == nil && !subTool.IsCommand() {
		args = &system.DefaultToolSchema
	}

	for _, existingTool := range completionTools {
		if existingTool.Function.ToolID == subTool.ID {
			return completionTools, nil
		}
	}

	if subTool.Instructions == "" {
		log.Debugf("Skipping zero instruction tool %s (%s)", subToolName, subTool.ID)
	} else {
		completionTools = append(completionTools, CompletionTool{
			Function: CompletionFunctionDefinition{
				ToolID:      subTool.ID,
				Name:        PickToolName(subToolName, toolNames),
				Description: subTool.Parameters.Description,
				Parameters:  args,
			},
		})
	}

	for _, export := range subTool.Export {
		completionTools, err = appendTool(completionTools, prg, subTool, export, toolNames)
		if err != nil {
			return nil, err
		}
	}

	return completionTools, nil
}

type Repo struct {
	// VCS The VCS type, such as "git"
	VCS string
	// The URL where the VCS repo can be found
	Root string
	// The path in the repo of this source. This should refer to a directory and not the actual file
	Path string
	// The filename of the source in the repo, relative to Path
	Name string
	// The revision of this source
	Revision string
}

type ToolSource struct {
	Location string `json:"location,omitempty"`
	LineNo   int    `json:"lineNo,omitempty"`
	Repo     *Repo  `json:"repo,omitempty"`
}

func (t ToolSource) String() string {
	return fmt.Sprintf("%s:%d", t.Location, t.LineNo)
}

func (t Tool) IsCommand() bool {
	return strings.HasPrefix(t.Instructions, CommandPrefix)
}

func (t Tool) IsDaemon() bool {
	return strings.HasPrefix(t.Instructions, DaemonPrefix)
}

func (t Tool) IsOpenAPI() bool {
	return strings.HasPrefix(t.Instructions, OpenAPIPrefix)
}

func (t Tool) IsHTTP() bool {
	return strings.HasPrefix(t.Instructions, "#!http://") ||
		strings.HasPrefix(t.Instructions, "#!https://")
}

func FirstSet[T comparable](in ...T) (result T) {
	for _, i := range in {
		if i != result {
			return i
		}
	}
	return
}
