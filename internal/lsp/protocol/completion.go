package protocol

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

// CompletionList represents a list of completion items
type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

// InitializeParams represents the parameters for the 'initialize' request
type InitializeParams struct {
	RootPath         string            `json:"rootPath,omitempty"`
	RootURI          string            `json:"rootUri,omitempty"`
	WorkspaceFolders []WorkspaceFolder `json:"workspaceFolders,omitempty"`
}

// WorkspaceFolder represents a workspace folder
type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// CompletionParams represents the parameters for a completion request
type CompletionParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	Position struct {
		Line      int `json:"line"`
		Character int `json:"character"`
	} `json:"position"`
	Context struct {
		// The completion trigger kind
		TriggerKind int `json:"triggerKind"`
		// The trigger character (a single character) that has trigger code complete.
		// Is undefined if `triggerKind !== CompletionTriggerKind.TriggerCharacter`
		TriggerCharacter string `json:"triggerCharacter,omitempty"`
	} `json:"context,omitempty"`
	// The position at which this completion was triggered.
	// This is used to filter the completion items based on their text edit range.
	WorkDoneToken interface{} `json:"workDoneToken,omitempty"`
	// An optional token that a server can use to report partial results (e.g. streaming) to
	// the client.
	PartialResultToken interface{} `json:"partialResultToken,omitempty"`

	// Custom fields for internal use (not part of LSP spec)
	// These fields are used to pass document content to completion providers
	DocumentContent []byte            `json:"-"`
	Node            *tree_sitter.Node `json:"-"`
}

// CompletionTriggerKind describes how a completion was triggered
type CompletionTriggerKind int

const (
	// Invoked - Completion was triggered by typing an identifier or similar
	Invoked CompletionTriggerKind = 1
	// TriggerCharacter - Completion was triggered by a trigger character
	TriggerCharacter CompletionTriggerKind = 2
	// TriggerForIncompleteCompletions - Completion was re-triggered as the current completion list is incomplete
	TriggerForIncompleteCompletions CompletionTriggerKind = 3
)

// CompletionItemKind describes the kind of a completion item
type CompletionItemKind int

const (
	TextCompletion          CompletionItemKind = 1
	MethodCompletion        CompletionItemKind = 2
	FunctionCompletion      CompletionItemKind = 3
	ConstructorCompletion   CompletionItemKind = 4
	FieldCompletion         CompletionItemKind = 5
	VariableCompletion      CompletionItemKind = 6
	ClassCompletion         CompletionItemKind = 7
	InterfaceCompletion     CompletionItemKind = 8
	ModuleCompletion        CompletionItemKind = 9
	PropertyCompletion      CompletionItemKind = 10
	UnitCompletion          CompletionItemKind = 11
	ValueCompletion         CompletionItemKind = 12
	EnumCompletion          CompletionItemKind = 13
	KeywordCompletion       CompletionItemKind = 14
	SnippetCompletion       CompletionItemKind = 15
	ColorCompletion         CompletionItemKind = 16
	FileCompletion          CompletionItemKind = 17
	ReferenceCompletion     CompletionItemKind = 18
	FolderCompletion        CompletionItemKind = 19
	EnumMemberCompletion    CompletionItemKind = 20
	ConstantCompletion      CompletionItemKind = 21
	StructCompletion        CompletionItemKind = 22
	EventCompletion         CompletionItemKind = 23
	OperatorCompletion      CompletionItemKind = 24
	TypeParameterCompletion CompletionItemKind = 25
)

// CompletionItem represents a completion item
type CompletionItem struct {
	// The label of this completion item
	Label string `json:"label"`

	// The kind of this completion item
	Kind int `json:"kind,omitempty"`

	// Tags for this completion item
	Tags []int `json:"tags,omitempty"`

	// A human-readable string with additional information about this item
	Detail string `json:"detail,omitempty"`

	// Documentation for this completion item
	Documentation struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	} `json:"documentation,omitempty"`

	// Indicates if this item is deprecated
	Deprecated bool `json:"deprecated,omitempty"`

	// Select this item when showing
	Preselect bool `json:"preselect,omitempty"`

	// A string that should be used when comparing this item with other items
	SortText string `json:"sortText,omitempty"`

	// A string that should be used when filtering a set of completion items
	FilterText string `json:"filterText,omitempty"`

	// A string that should be inserted into a document when selecting this completion
	InsertText string `json:"insertText,omitempty"`

	// The format of the insert text. The format applies to both the `insertText` property
	// and the `newText` property of a provided `textEdit`
	InsertTextFormat int `json:"insertTextFormat,omitempty"`

	// How whitespace and indentation is handled during completion item insertion
	InsertTextMode int `json:"insertTextMode,omitempty"`

	// An edit which is applied to a document when selecting this completion
	TextEdit interface{} `json:"textEdit,omitempty"`

	// Additional text edits that should be applied when selecting this completion
	AdditionalTextEdits []interface{} `json:"additionalTextEdits,omitempty"`

	// Command that is executed after the completion item is selected
	Command interface{} `json:"command,omitempty"`

	// A data entry field that is preserved on a completion item between a completion and a completion resolve request
	Data interface{} `json:"data,omitempty"`

	// Indicates which part of the text should be selected after insertion
	TextEditText string `json:"textEditText,omitempty"`
}
