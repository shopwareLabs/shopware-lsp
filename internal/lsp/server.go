package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shopware/shopware-lsp/internal/indexer"
	"github.com/shopware/shopware-lsp/internal/lsp/protocol"
	"github.com/sourcegraph/jsonrpc2"
)

// Server represents the LSP server
type Server struct {
	rootPath             string
	conn                 *jsonrpc2.Conn
	completionProviders  []CompletionProvider
	definitionProviders  []GotoDefinitionProvider
	referencesProviders  []ReferencesProvider
	codeLensProviders    []CodeLensProvider
	diagnosticsProviders []DiagnosticsProvider
	codeActionProviders  []CodeActionProvider
	hoverProviders       []HoverProvider
	commandProviders     []CommandProvider
	indexers             map[string]indexer.Indexer
	commandMap           map[string]CommandFunc
	indexerMu            sync.RWMutex
	documentManager      *DocumentManager
	fileScanner          *indexer.FileScanner
	cacheDir             string
	version              string
}

// NewServer creates a new LSP server
func NewServer(filescanner *indexer.FileScanner, cacheDir, version string) *Server {
	s := &Server{
		completionProviders:  make([]CompletionProvider, 0),
		definitionProviders:  make([]GotoDefinitionProvider, 0),
		referencesProviders:  make([]ReferencesProvider, 0),
		codeLensProviders:    make([]CodeLensProvider, 0),
		diagnosticsProviders: make([]DiagnosticsProvider, 0),
		codeActionProviders:  make([]CodeActionProvider, 0),
		hoverProviders:       make([]HoverProvider, 0),
		commandProviders:     make([]CommandProvider, 0),
		indexers:             make(map[string]indexer.Indexer),
		commandMap:           make(map[string]CommandFunc),
		documentManager:      NewDocumentManager(),
		fileScanner:          filescanner,
		cacheDir:             cacheDir,
		version:              version,
	}

	// Set the update callback to publish diagnostics
	s.fileScanner.SetOnUpdate(func() {
		log.Printf("Publishing diagnostics to all open files")
		go s.PublishDiagnostics(context.Background(), nil)
	})

	return s
}

// RegisterCompletionProvider registers a completion provider with the server
func (s *Server) RegisterCompletionProvider(provider CompletionProvider) {
	s.completionProviders = append(s.completionProviders, provider)
}

// RegisterDefinitionProvider registers a definition provider with the server
func (s *Server) RegisterDefinitionProvider(provider GotoDefinitionProvider) {
	s.definitionProviders = append(s.definitionProviders, provider)
}

// RegisterReferencesProvider registers a references provider with the server
func (s *Server) RegisterReferencesProvider(provider ReferencesProvider) {
	s.referencesProviders = append(s.referencesProviders, provider)
}

// RegisterCodeLensProvider registers a code lens provider with the server
func (s *Server) RegisterCodeLensProvider(provider CodeLensProvider) {
	s.codeLensProviders = append(s.codeLensProviders, provider)
}

// RegisterCodeActionProvider registers a code action provider with the server
func (s *Server) RegisterCodeActionProvider(provider CodeActionProvider) {
	s.codeActionProviders = append(s.codeActionProviders, provider)
}

// RegisterHoverProvider registers a hover provider with the server
func (s *Server) RegisterHoverProvider(provider HoverProvider) {
	s.hoverProviders = append(s.hoverProviders, provider)
}

// RegisterCommandProvider registers a command provider with the server
func (s *Server) RegisterCommandProvider(provider CommandProvider) {
	s.commandProviders = append(s.commandProviders, provider)
}

// RegisterIndexer adds an indexer to the registry
func (s *Server) RegisterIndexer(indexer indexer.Indexer, err error) {
	s.indexerMu.Lock()
	defer s.indexerMu.Unlock()
	s.indexers[indexer.ID()] = indexer
	s.fileScanner.AddIndexer(indexer)
}

// GetIndexer retrieves an indexer by ID
func (s *Server) GetIndexer(id string) (indexer.Indexer, bool) {
	s.indexerMu.RLock()
	defer s.indexerMu.RUnlock()
	indexer, ok := s.indexers[id]
	return indexer, ok
}

// shouldForceReindex checks if the current version differs from the last run
// and updates the stored version file
func (s *Server) shouldForceReindex() (bool, error) {
	if s.cacheDir == "" || s.version == "" || s.version == "dev" {
		return false, nil
	}

	versionFile := filepath.Join(s.cacheDir, "version.txt")

	// Check if version file exists
	previousVersion := ""
	forceReindex := false

	data, err := os.ReadFile(versionFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("failed to read version file: %w", err)
		}
		// File doesn't exist, will create it below
		forceReindex = true
	} else {
		previousVersion = strings.TrimSpace(string(data))
		forceReindex = previousVersion != s.version
	}

	// Update the version file with current version
	if err := os.WriteFile(versionFile, []byte(s.version), 0644); err != nil {
		return forceReindex, fmt.Errorf("failed to write version file: %w", err)
	}

	return forceReindex, nil
}

// indexAll builds or updates all registered indexes
// If forceReindex is true, it will clear the existing index before rebuilding
func (s *Server) indexAll(ctx context.Context, forceReindex bool) error {
	startTime := time.Now()

	// Send notification that indexing has started
	if s.conn != nil {
		if err := s.conn.Notify(ctx, "shopware/indexingStarted", map[string]interface{}{
			"message": "Indexing started",
		}); err != nil {
			return err
		}
	}

	if forceReindex {
		if err := s.fileScanner.ClearHashes(); err != nil {
			return err
		}
	}

	if err := s.fileScanner.IndexAll(ctx); err != nil {
		return err
	}

	elapsedTime := time.Since(startTime)

	// Send notification that indexing has completed
	if s.conn != nil {
		if err := s.conn.Notify(ctx, "shopware/indexingCompleted", map[string]interface{}{
			"message":       "Indexing completed",
			"timeInSeconds": elapsedTime.Seconds(),
		}); err != nil {
			return err
		}
	}

	return nil
}

// CloseAll closes all registered indexers and resources
func (s *Server) CloseAll() error {
	// Close document manager first
	if s.documentManager != nil {
		s.documentManager.Close()
	}

	// Then close all indexers
	s.indexerMu.RLock()
	defer s.indexerMu.RUnlock()

	for _, indexer := range s.indexers {
		if err := indexer.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) Start(in io.Reader, out io.Writer) error {
	// Register commands
	for _, provider := range s.commandProviders {
		for command, fn := range provider.GetCommands(context.Background()) {
			s.commandMap[command] = fn
		}
	}

	// Create a new JSON-RPC connection
	stream := jsonrpc2.NewBufferedStream(rwc{in, out}, jsonrpc2.VSCodeObjectCodec{})
	conn := jsonrpc2.NewConn(context.Background(), stream, jsonrpc2.HandlerWithError(s.handle))
	s.conn = conn

	// Wait for the connection to close
	<-conn.DisconnectNotify()

	return nil
}

// rwc combines a reader and writer into a single ReadWriteCloser
type rwc struct {
	io.Reader
	io.Writer
}

// Close implements io.Closer
func (rwc) Close() error {
	return nil
}

// handle processes incoming JSON-RPC requests and notifications
func (s *Server) handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) (interface{}, error) {
	// Handle exit notification after shutdown
	if req.Method == "exit" {
		log.Println("Received exit notification, exiting")
		if err := conn.Close(); err != nil {
			log.Printf("error closing connection: %v", err)
		}
		return nil, nil
	}

	if cmd, ok := s.commandMap[req.Method]; ok {
		return cmd(ctx, req.Params)
	}

	switch req.Method {
	case "initialize":
		var params protocol.InitializeParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, &jsonrpc2.Error{Code: jsonrpc2.CodeParseError, Message: err.Error()}
		}
		return s.initialize(ctx, &params), nil

	case "initialized":
		// Build the index when the client is initialized
		go func() {
			// Check if we need to force reindex due to version change
			forceReindex, err := s.shouldForceReindex()
			if err != nil {
				log.Printf("Warning: Failed to check version for reindex: %v", err)
			}

			if forceReindex {
				log.Printf("Version changed to %s, forcing reindex", s.version)
			}

			// Index all registered indexers
			if err := s.indexAll(ctx, forceReindex); err != nil {
				log.Printf("Error indexing: %v", err)
			} else if forceReindex {
				log.Println("Force reindex completed successfully")
			}

			// Start the file watcher after the initial index build to avoid
			// paying for two recursive traversals during startup.
			if err := s.fileScanner.StartWatcher(); err != nil {
				log.Printf("Error starting file watcher: %v", err)
			} else {
				log.Println("File watcher started successfully")
			}
		}()
		return nil, nil

	case "textDocument/didOpen":
		var params struct {
			TextDocument struct {
				URI     string `json:"uri"`
				Text    string `json:"text"`
				Version int    `json:"version"`
			} `json:"textDocument"`
		}
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, err
		}
		s.documentManager.OpenDocument(params.TextDocument.URI, params.TextDocument.Text, params.TextDocument.Version)

		// Run diagnostics on the opened document
		go s.publishDiagnostics(ctx, params.TextDocument.URI, params.TextDocument.Version)
		return nil, nil

	case "textDocument/didChange":
		var params struct {
			TextDocument struct {
				URI     string `json:"uri"`
				Version int    `json:"version"`
			} `json:"textDocument"`
			ContentChanges []struct {
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, err
		}
		if len(params.ContentChanges) > 0 {
			s.documentManager.UpdateDocument(params.TextDocument.URI, params.ContentChanges[0].Text, params.TextDocument.Version)

			// Run diagnostics on the updated document
			go s.publishDiagnostics(ctx, params.TextDocument.URI, params.TextDocument.Version)
		}
		return nil, nil

	case "textDocument/didClose":
		var params struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, err
		}
		s.documentManager.CloseDocument(params.TextDocument.URI)
		return nil, nil

	case "textDocument/completion":
		var params protocol.CompletionParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, err
		}

		return s.completion(ctx, &params), nil

	case "textDocument/definition":
		var params protocol.DefinitionParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, err
		}
		return s.definition(ctx, &params), nil

	case "textDocument/references":
		var params protocol.ReferenceParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, err
		}
		return s.references(ctx, &params), nil

	case "textDocument/codeLens":
		var params protocol.CodeLensParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, err
		}
		return s.codeLens(ctx, &params), nil

	case "textDocument/hover":
		var params protocol.HoverParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, err
		}
		return s.hover(ctx, &params)

	case "textDocument/diagnostic":
		var params protocol.DiagnosticParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, err
		}
		return s.diagnostic(ctx, &params), nil

	case "codeLens/resolve":
		var codeLens protocol.CodeLens
		if err := json.Unmarshal(*req.Params, &codeLens); err != nil {
			return nil, err
		}
		return s.resolveCodeLens(ctx, &codeLens)

	case "textDocument/codeAction":
		var params protocol.CodeActionParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, err
		}
		return s.codeAction(ctx, &params), nil

	case "shopware/forceReindex":
		// Force reindex all indexers
		go func() {
			if err := s.indexAll(ctx, true); err != nil {
				log.Printf("Error force reindexing: %v", err)
			}
		}()
		return map[string]interface{}{
			"message": "Force reindexing started",
		}, nil

	case "shutdown":
		// Clean up resources
		if err := s.CloseAll(); err != nil {
			log.Printf("Error closing indexers: %v", err)
		}

		log.Println("Received shutdown request, waiting for exit notification")
		return nil, nil

	case "workspace/didCreateFiles":
		var params protocol.CreateFilesParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, err
		}

		files := make([]string, len(params.Files))
		for i, file := range params.Files {
			files[i] = strings.TrimPrefix(file.URI, "file://")
		}
		if err := s.fileScanner.IndexFiles(ctx, files); err != nil {
			log.Printf("Error indexing new files: %v", err)
		}

		log.Printf("Watcher Client: Created files: %v", files)

		return nil, nil

	case "workspace/didRenameFiles":
		var params protocol.RenameFilesParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, err
		}

		oldFiles := make([]string, len(params.Files))
		newFiles := make([]string, len(params.Files))
		for i, file := range params.Files {
			oldFiles[i] = strings.TrimPrefix(file.OldURI, "file://")
			newFiles[i] = strings.TrimPrefix(file.NewURI, "file://")
		}

		if err := s.fileScanner.IndexFiles(ctx, newFiles); err != nil {
			log.Printf("Error indexing new files: %v", err)
		}
		if err := s.fileScanner.RemoveFiles(ctx, oldFiles); err != nil {
			log.Printf("Error removing old files: %v", err)
		}

		log.Printf("Watcher Client: Renamed files: %v", oldFiles)

		return nil, nil

	case "workspace/didDeleteFiles":
		var params protocol.DeleteFilesParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, err
		}

		files := make([]string, len(params.Files))
		for i, file := range params.Files {
			files[i] = strings.TrimPrefix(file.URI, "file://")
		}

		log.Printf("Watcher Client: Deleting files: %v", files)

		if err := s.fileScanner.RemoveFiles(ctx, files); err != nil {
			log.Printf("Error removing old files: %v", err)
		}
		return nil, nil

	case "workspace/didChangeWatchedFiles":
		var params protocol.DidChangeWatchedFilesParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, err
		}

		createFiles := []string{}
		deleteFiles := []string{}

		// Handle file change events
		for _, change := range params.Changes {
			switch change.Type {
			case int(protocol.FileCreated):
				createFiles = append(createFiles, strings.TrimPrefix(change.URI, "file://"))
			case int(protocol.FileChanged):
				createFiles = append(createFiles, strings.TrimPrefix(change.URI, "file://"))
			case int(protocol.FileDeleted):
				deleteFiles = append(deleteFiles, strings.TrimPrefix(change.URI, "file://"))
			}
		}

		if len(createFiles) > 0 {
			log.Printf("Watcher Client: Creating files: %v", createFiles)

			if err := s.fileScanner.IndexFiles(ctx, createFiles); err != nil {
				log.Printf("Error indexing new files: %v", err)
			}
		}

		if len(deleteFiles) > 0 {
			log.Printf("Watcher Client: Deleting files: %v", deleteFiles)

			if err := s.fileScanner.RemoveFiles(ctx, deleteFiles); err != nil {
				log.Printf("Error removing old files: %v", err)
			}
		}

		return nil, nil

	default:
		// Check if this is a notification (no ID)
		if req.ID == (jsonrpc2.ID{}) {
			// This is a notification, no response needed
			return nil, nil
		}
		return nil, &jsonrpc2.Error{Code: jsonrpc2.CodeMethodNotFound, Message: "Method not implemented: " + req.Method}
	}
}

// initialize handles the LSP initialize request
func (s *Server) initialize(ctx context.Context, params *protocol.InitializeParams) interface{} {
	// Extract root path from params
	s.extractRootPath(params)

	// Collect all trigger characters from providers
	triggerChars := s.collectTriggerCharacters()

	// Collect all code action kinds from providers
	codeActionKinds := s.collectCodeActionKinds()

	// Define server capabilities
	return map[string]interface{}{
		"capabilities": map[string]interface{}{
			"textDocumentSync": map[string]interface{}{
				"openClose": true,
				"change":    1, // Full sync
			},
			"diagnosticProvider": map[string]interface{}{
				"interFileDependencies": true,
				"workspaceDiagnostics":  false,
			},
			"completionProvider": map[string]interface{}{
				"triggerCharacters": triggerChars,
			},
			"definitionProvider": true,
			"referencesProvider": true,
			"hoverProvider":      true,
			"codeLensProvider": map[string]interface{}{
				"resolveProvider": true,
			},
			"codeActionProvider": map[string]interface{}{
				"codeActionKinds": codeActionKinds,
			},
			"workspace": map[string]interface{}{
				"fileOperations": map[string]interface{}{
					"didCreate": map[string]interface{}{
						"filters": []map[string]interface{}{
							{"pattern": map[string]interface{}{"glob": "**/*.xml"}},
							{"pattern": map[string]interface{}{"glob": "**/*.php"}},
						},
					},
					"didRename": map[string]interface{}{
						"filters": []map[string]interface{}{
							{"pattern": map[string]interface{}{"glob": "**/*.xml"}},
							{"pattern": map[string]interface{}{"glob": "**/*.php"}},
						},
					},
					"didDelete": map[string]interface{}{
						"filters": []map[string]interface{}{
							{"pattern": map[string]interface{}{"glob": "**/*.xml"}},
							{"pattern": map[string]interface{}{"glob": "**/*.php"}},
						},
					},
				},
			},
		},
	}
}

// extractRootPath extracts the root path from the initialize params
func (s *Server) extractRootPath(params *protocol.InitializeParams) {
	// Try to get from RootPath
	if params.RootPath != "" {
		s.rootPath = params.RootPath
		return
	}

	// Try to get from RootURI
	if params.RootURI != "" {
		rootURI := params.RootURI
		s.rootPath = strings.TrimPrefix(rootURI, "file://")
		return
	}

	// Try to get from WorkspaceFolders
	if len(params.WorkspaceFolders) > 0 {
		folder := params.WorkspaceFolders[0]
		s.rootPath = strings.TrimPrefix(folder.URI, "file://")
		return
	}

	// Fall back to current directory
	s.rootPath, _ = os.Getwd()
}

// collectTriggerCharacters collects all trigger characters from registered providers
func (s *Server) collectTriggerCharacters() []string {
	// Use a map to deduplicate trigger characters
	triggerCharsMap := make(map[string]bool)

	for _, provider := range s.completionProviders {
		for _, char := range provider.GetTriggerCharacters() {
			triggerCharsMap[char] = true
		}
	}

	// Convert map keys to slice
	triggerChars := make([]string, 0, len(triggerCharsMap))
	for char := range triggerCharsMap {
		triggerChars = append(triggerChars, char)
	}

	return triggerChars
}

// collectCodeActionKinds collects all code action kinds from registered providers
func (s *Server) collectCodeActionKinds() []protocol.CodeActionKind {
	// Use a map to deduplicate code action kinds
	kindsMap := make(map[protocol.CodeActionKind]bool)

	for _, provider := range s.codeActionProviders {
		for _, kind := range provider.GetCodeActionKinds() {
			kindsMap[kind] = true
		}
	}

	// Convert map keys to slice
	kinds := make([]protocol.CodeActionKind, 0, len(kindsMap))
	for kind := range kindsMap {
		kinds = append(kinds, kind)
	}

	return kinds
}

func (s *Server) DocumentManager() *DocumentManager {
	return s.documentManager
}

func (s *Server) FileScanner() *indexer.FileScanner {
	return s.fileScanner
}

// RegisterDiagnosticsProvider registers a diagnostics provider with the server
func (s *Server) RegisterDiagnosticsProvider(provider DiagnosticsProvider) {
	s.diagnosticsProviders = append(s.diagnosticsProviders, provider)
}

type docAnalyse struct {
	uri     string
	version int
}

func (s *Server) PublishDiagnostics(ctx context.Context, files []string) {
	var docs []docAnalyse

	if files == nil {
		for _, doc := range s.DocumentManager().documents {
			docs = append(docs, docAnalyse{
				uri:     doc.URI,
				version: doc.Version,
			})
		}
	} else {
		for _, uri := range files {
			version := 0

			if doc, ok := s.DocumentManager().GetDocument(uri); ok {
				version = doc.Version
			}

			docs = append(docs, docAnalyse{
				uri:     uri,
				version: version,
			})
		}
	}

	for _, doc := range docs {
		go s.publishDiagnostics(ctx, doc.uri, doc.version)
	}
}

// publishDiagnostics collects and publishes diagnostics for a document
func (s *Server) publishDiagnostics(ctx context.Context, uri string, version int) {
	if s.conn == nil {
		return
	}

	// Get document content
	content, ok := s.documentManager.GetDocumentText(uri)
	if !ok {
		return
	}

	// Collect diagnostics from all providers
	allDiagnostics := []protocol.Diagnostic{}

	node := s.documentManager.GetRootNode(uri)

	if node == nil {
		return
	}

	for _, provider := range s.diagnosticsProviders {
		diagnostics, err := provider.GetDiagnostics(ctx, uri, node, content)
		if err != nil {
			log.Printf("Error getting diagnostics from provider %s: %v", provider, err)
			continue
		}

		allDiagnostics = append(allDiagnostics, diagnostics...)
	}

	// Publish diagnostics
	params := protocol.PublishDiagnosticsParams{
		URI:         uri,
		Version:     version,
		Diagnostics: allDiagnostics,
	}

	if err := s.conn.Notify(ctx, "textDocument/publishDiagnostics", params); err != nil {
		log.Printf("Error publishing diagnostics: %v", err)
	}
}

// diagnostic handles textDocument/diagnostic requests
func (s *Server) diagnostic(ctx context.Context, params *protocol.DiagnosticParams) interface{} {
	uri := params.TextDocument.URI

	// Get document content
	content, ok := s.documentManager.GetDocumentText(uri)
	if !ok {
		return protocol.DiagnosticResult{
			Items: []protocol.Diagnostic{},
		}
	}

	// Collect diagnostics from all providers
	allDiagnostics := []protocol.Diagnostic{}

	node := s.documentManager.GetRootNode(uri)

	if node == nil {
		return protocol.DiagnosticResult{
			Items: []protocol.Diagnostic{},
		}
	}

	for _, provider := range s.diagnosticsProviders {
		diagnostics, err := provider.GetDiagnostics(ctx, uri, node, content)
		if err != nil {
			log.Printf("Error getting diagnostics from provider %s: %v", provider, err)
			continue
		}

		allDiagnostics = append(allDiagnostics, diagnostics...)
	}

	return protocol.DiagnosticResult{
		Items: allDiagnostics,
	}
}

// codeAction handles textDocument/codeAction requests
func (s *Server) codeAction(ctx context.Context, params *protocol.CodeActionParams) []protocol.CodeAction {
	node, docText, ok := s.documentManager.GetNodeAtPosition(params.TextDocument.URI, params.Range.Start.Line, params.Range.Start.Character)
	if ok {
		params.Node = node
		params.DocumentContent = docText.Text
	}

	// Collect code actions from all providers
	var allCodeActions []protocol.CodeAction
	for _, provider := range s.codeActionProviders {
		codeActions := provider.GetCodeActions(ctx, params)
		allCodeActions = append(allCodeActions, codeActions...)
	}

	return allCodeActions
}
