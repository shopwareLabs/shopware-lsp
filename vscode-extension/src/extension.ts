import * as path from 'path';
import * as vscode from 'vscode';
import { ClientManager } from './clientManager';

const languages = ['php', 'xml', 'yml', 'yaml', 'twig', 'json', 'scss'];

let clients: ClientManager;
// Add a status bar item for indexing status
let indexingStatusBarItem: vscode.StatusBarItem;

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  // Create output channel for the language server
  const outputChannel = vscode.window.createOutputChannel("Shopware LSP");
  context.subscriptions.push(outputChannel);
  
  // Create status bar item for indexing status
  indexingStatusBarItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
  context.subscriptions.push(indexingStatusBarItem);

  clients = new ClientManager(context, outputChannel);

  async function startClient(document: vscode.TextDocument) {
    const workspaceRoot = vscode.workspace.getWorkspaceFolder(document.uri);

    if (!workspaceRoot || !languages.includes(document.languageId) || document.uri.scheme !== 'file') {
      return;
    }

    clients.start(workspaceRoot).then((client) => {
      if (!client) {
        return;
      }

      // Handler for indexing started
      client.onNotification('shopware/indexingStarted', () => {
        outputChannel.appendLine('Shopware indexing started');
        indexingStatusBarItem.text = '$(sync~spin) Shopware: Indexing...';
        indexingStatusBarItem.tooltip = 'Shopware language server is currently indexing';
        indexingStatusBarItem.show();
      });
      
      // Handler for indexing completed
      client.onNotification('shopware/indexingCompleted', (params: { timeInSeconds: number }) => {
        indexingStatusBarItem.text = `$(check) Shopware: Indexed`;
        indexingStatusBarItem.tooltip = `Indexing completed in ${params.timeInSeconds} seconds`;
        
        // Hide the status bar message after 10 seconds
        setTimeout(() => {
          indexingStatusBarItem.hide();
        }, 10000);
      });
    }).catch((err: Error) => {
      outputChannel.appendLine(`Error registering notification handler: ${err}`);
    });
  }

  async function startAllClients() {
    // Stop all existing clients 
    await clients.stop();
    // Start clients for all opened text documents
    await Promise.all(vscode.workspace.textDocuments.map(startClient));
  }

  vscode.workspace.onDidOpenTextDocument(startClient);
  vscode.workspace.onDidChangeWorkspaceFolders((event) => {
		for (const folder of event.removed) {
      // Stop clients for removed workspace folders
      clients.stop(folder);
		}
	});

  await startAllClients();

  // Register restart command
  context.subscriptions.push(vscode.commands.registerCommand('shopwareLSP.restart', async () => {
    await startAllClients();
    vscode.window.showInformationMessage('Shopware LSP restarted');
  }));

  // Register force reindex command
  context.subscriptions.push(vscode.commands.registerCommand('shopwareLSP.forceReindex', async () => {
    let client = clients.getActive();

    if (!client) {
      vscode.window.showErrorMessage('Shopware LSP is not running');
      return;
    }
    
    try {
      const result = await client.sendRequest('shopware/forceReindex');
      vscode.window.showInformationMessage('Shopware LSP: Force reindexing started');
    } catch (error) {
      vscode.window.showErrorMessage(`Failed to trigger force reindexing: ${error}`);
    }
  }));

  // Register open references command
  context.subscriptions.push(vscode.commands.registerCommand('shopware.openReferences', async (references: string[]) => {
    if (!references || references.length === 0) {
      vscode.window.showInformationMessage('No references found');
      return;
    }

    // Create quick pick items from references
    const items = references.map(ref => {
      // Parse the URI and line number from the reference (format: file:///path/to/file.twig#lineNumber)
      const [uri, lineStr] = ref.split('#');
      const line = parseInt(lineStr, 10) - 1; // Convert to 0-based line number
      const filePath = uri.replace('file://', '');
      
      // Extract relative path from workspace root if possible
      let displayPath = filePath;
      const workspaceFolders = vscode.workspace.workspaceFolders;
      if (workspaceFolders && workspaceFolders.length > 0) {
        const workspaceRoot = workspaceFolders[0].uri.fsPath;
        if (filePath.startsWith(workspaceRoot)) {
          displayPath = filePath.substring(workspaceRoot.length + 1); // +1 to remove the leading slash
        }
      }
      
      return {
        label: `$(file) ${path.basename(filePath)}`,
        description: displayPath,
        detail: `Line ${line + 1}`,
        uri,
        line
      };
    });

    // If there's only one reference, directly open it without showing the quick pick
    if (items.length === 1) {
      const item = items[0];
      const document = await vscode.workspace.openTextDocument(vscode.Uri.parse(item.uri));
      const editor = await vscode.window.showTextDocument(document);
      
      // Position at the specified line
      const position = new vscode.Position(item.line, 0);
      editor.selection = new vscode.Selection(position, position);
      editor.revealRange(
        new vscode.Range(position, position),
        vscode.TextEditorRevealType.InCenter
      );
      return;
    }

    // Show quick pick with references when there are multiple
    const selected = await vscode.window.showQuickPick(items, {
      placeHolder: 'Select a reference to open',
      matchOnDescription: true,
      matchOnDetail: true
    });

    if (selected) {
      // Open the selected file and position at the specified line
      const document = await vscode.workspace.openTextDocument(vscode.Uri.parse(selected.uri));
      const editor = await vscode.window.showTextDocument(document);
      
      // Position at the specified line
      const position = new vscode.Position(selected.line, 0);
      editor.selection = new vscode.Selection(position, position);
      editor.revealRange(
        new vscode.Range(position, position),
        vscode.TextEditorRevealType.InCenter
      );
    }
  }));
  
  // Register create snippet command handler
  context.subscriptions.push(vscode.commands.registerCommand('shopware.createSnippet', async (snippetKey: string, fileUri: string) => {
    let client = clients.getActive();
    
    try {
      if (!client) {
        vscode.window.showErrorMessage('Shopware LSP is not running');
        return;
      }
      
      const result = await client.sendRequest<{paths: SnippetFile[]}>('shopware/snippet/getPossibleSnippetFilse', {
        fileUri,
      });

      if (!result || !result.paths || result.paths.length === 0) {
        vscode.window.showErrorMessage('No snippet files found');
        return;
      }

      let firstValue = '';

      for (const snippetFile of result.paths) {
        const result = await vscode.window.showInputBox({
          prompt: `Enter snippet value for ${snippetFile.name}`,
          value: firstValue,
          title: `Create snippet for ${snippetKey}`,
        })

        if (result === undefined) {
          vscode.window.showErrorMessage('Snippet creation cancelled');
          return;
        }

        if (result.trim() === '') {
          vscode.window.showErrorMessage('Snippet value cannot be empty');
          return;
        }

        firstValue = result;
        snippetFile.value = result;
      }

      await client.sendRequest('shopware/snippet/create', {
        fileUri,
        snippetKey,
        snippets: result.paths
      });

      vscode.window.showInformationMessage(`Snippet ${snippetKey} created successfully`);
    } catch (error) {
      vscode.window.showErrorMessage(`Error creating snippet: ${error}`);
    }
  }));

  context.subscriptions.push(vscode.commands.registerCommand('shopware.twig.extendBlock', async (textUri: string, blockName: string) => {
    let client = clients.getActive();
    
    if (!client) {
      vscode.window.showErrorMessage('Shopware LSP is not running');
      return;
    }

    const extensions: { Name: string; }[] = await client.sendRequest('shopware/extension/all');

    if (!extensions || extensions.length === 0) {
      vscode.window.showErrorMessage('No extensions found');
      return;
    }

    const items = extensions.map(ext => ({
      label: ext.Name,
      description: `Extend block in ${ext.Name}`,
      detail: `Block name: ${blockName}`,
    }));
    const selected = await vscode.window.showQuickPick(items, {
      placeHolder: 'Select an extension to extend the block',
      matchOnDescription: true,
      matchOnDetail: true
    });
    if (!selected) {
      vscode.window.showErrorMessage('No extension selected');
      return;
    }

    const result: {code: string, message: string} | {uri: string, line: number} = await client.sendRequest('shopware/twig/extendBlock', {
      textUri,
      blockName,
      extension: selected.label,
    });

    if ('code' in result) {
      vscode.window.showErrorMessage(`Error extending block: ${result.message}`);
      return;
    }

    if ('uri' in result) {
      const document = await vscode.workspace.openTextDocument(vscode.Uri.parse(result.uri));
      const editor = await vscode.window.showTextDocument(document);

      const position = new vscode.Position(result.line, 0);
      editor.selection = new vscode.Selection(position, position);

      vscode.window.showInformationMessage(`Block ${blockName} extended successfully in ${selected.label}`);
    }
  }));

  context.subscriptions.push(vscode.commands.registerCommand('shopware.insertSnippet', async () => {
    let client = clients.getActive();
    
    if (!client) {
      vscode.window.showErrorMessage('Shopware LSP is not running');
      return;
    }

    const snippets: {key: string, text: string, file: string}[] = await client.sendRequest('shopware/snippet/all');

    if (!snippets || snippets.length === 0) {
      vscode.window.showErrorMessage('No snippets found');
      return;
    }

    const items = snippets.map(snippet => ({
      label: snippet.key,
      description: snippet.text,
    }));

    const selected = await vscode.window.showQuickPick(items, {
      placeHolder: 'Select a snippet to insert',
      matchOnDescription: true,
      matchOnDetail: true
    });
    if (!selected) {
      return;
    }

    const text = `{{ '${selected.label}'|trans }}`;

    const editor = vscode.window.activeTextEditor;
    if (editor) {
      editor.insertSnippet(new vscode.SnippetString(text));
    }
  }));

  context.subscriptions.push(vscode.commands.registerCommand('shopware.createSnippetFromSelection', async (fileUri: string, selectedText: string) => {
    let client = clients.getActive();
    
    try {
      if (!client) {
        vscode.window.showErrorMessage('Shopware LSP is not running');
        return;
      }

      // Ask for snippet key
      const snippetKey = await vscode.window.showInputBox({
        prompt: 'Enter a key for the snippet',
        placeHolder: 'e.g. my-component.title',
        validateInput: (value: string) => {
          if (!value || value.trim() === '') {
            return 'Snippet key cannot be empty';
          }
          return null;
        }
      });

      if (!snippetKey) {
        return; // User cancelled
      }

      // Get possible snippet files
      const result = await client.sendRequest<{paths: SnippetFile[]}>('shopware/snippet/getPossibleSnippetFilse', {
        fileUri,
      });

      if (!result || !result.paths || result.paths.length === 0) {
        vscode.window.showErrorMessage('No snippet files found');
        return;
      }

      // Set the selected text as value for all snippet files
      for (const snippetFile of result.paths) {
        snippetFile.value = selectedText;
      }

      // Create the snippet
      await client.sendRequest('shopware/snippet/create', {
        fileUri,
        snippetKey,
        snippets: result.paths
      });

      // Get the editor
      const editor = vscode.window.activeTextEditor;
      if (editor) {
        // Replace the selected text with the snippet reference
        const snippetReference = `{{ '${snippetKey}'|trans }}`;
        editor.edit(editBuilder => {
          const selection = editor.selection;
          editBuilder.replace(selection, snippetReference);
        });
      }

      vscode.window.showInformationMessage(`Snippet ${snippetKey} created successfully`);
    } catch (error) {
      vscode.window.showErrorMessage(`Error creating snippet from selection: ${error}`);
    }
  }));
}

export function deactivate(): Thenable<void> | undefined {
  if (clients.isEmpty()) {
    return undefined;
  }
  
  // Add a timeout to ensure the server has time to respond
  return new Promise<void>((resolve) => {
    // Try to stop the client gracefully
    const stopPromise = clients.stop();
    
    // Set a timeout in case the stop hangs
    const timeout = setTimeout(() => {
      console.log('Client stop timed out, forcing resolution');
      resolve();
    }, 2000); // 2 second timeout
    
    // Handle normal completion
    stopPromise.then(() => {
      clearTimeout(timeout);
      resolve();
    }).catch(error => {
      console.error('Error stopping client:', error);
      clearTimeout(timeout);
      resolve(); // Resolve anyway to prevent VSCode from hanging
    });
  });
}