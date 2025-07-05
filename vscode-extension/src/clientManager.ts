import * as path from 'path';
import * as fs from 'fs';
import * as vscode from 'vscode';
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
  TransportKind,
  RevealOutputChannelOn
} from 'vscode-languageclient/node';

export class ClientManager {
    private _sortedWorkspaceFolders: string[] | undefined;
    private clients = new Map<string, LanguageClient>()

    constructor(
        private context: vscode.ExtensionContext,
        private outputChannel: vscode.OutputChannel, 
    ) {
        vscode.workspace.onDidChangeWorkspaceFolders(() => this._sortedWorkspaceFolders = undefined);
    }

    public async start(workspaceRoot: vscode.WorkspaceFolder, restart: boolean = false): Promise<LanguageClient | false> {
        const folder = this.getOuterMostWorkspaceFolder(workspaceRoot);
        const key = folder.uri.toString();

        const existingClient = this.clients.get(key);
        if (existingClient) {
            if (!restart) {
                return Promise.resolve(existingClient);
            }

            this.clients.delete(key);
            await existingClient.stop();
        }

        const serverPath = this.getServerPath(workspaceRoot);
        
        if (!serverPath) {
            vscode.window.showErrorMessage(`[${key}] Could not find Symfony Service LSP server. Please set the path in settings.`);
            return false;
        }
        
        const client = this.createClient(serverPath, folder);

        this.outputChannel.appendLine(`[${key}] Starting Shopware Language Server at ${serverPath}`);
    
        this.clients.set(key, client);

        return client.start().then(() => client);
    }

    public async stop(workspaceRoot: vscode.WorkspaceFolder | undefined = undefined): Promise<void> {
        if (!workspaceRoot) {
            await Promise.all([...this.clients.values()].map((client) => client.stop()));
            this.clients.clear();
        } else {
            const folder = this.getOuterMostWorkspaceFolder(workspaceRoot);
            const key = folder.uri.toString();

            const client = this.clients.get(key);
            if (client) {
                await client.stop();
                this.clients.delete(key);
            }
        }
    }

    public get(workspaceRoot: vscode.WorkspaceFolder): LanguageClient | undefined {
        const folder = this.getOuterMostWorkspaceFolder(workspaceRoot);
        const key = folder.uri.toString();
        return this.clients.get(key);
    }

    public getActive(): LanguageClient | undefined {
        const activeEditor = vscode.window.activeTextEditor;
        if (!activeEditor) {
            return undefined;
        }

        const workspaceFolder = vscode.workspace.getWorkspaceFolder(activeEditor.document.uri);
        if (!workspaceFolder) {
            return undefined;
        }

        return this.get(workspaceFolder);
    }

    public isEmpty(): boolean {
        return this.clients.size === 0;
    }

    private createClient(serverPath: string, folder: vscode.WorkspaceFolder): LanguageClient {
        // Define server options
        const serverOptions: ServerOptions = {
          command: serverPath,
          args: [],
          transport: TransportKind.stdio
        };

        // Define client options
        const clientOptions: LanguageClientOptions = {
          documentSelector: [
            { scheme: 'file', language: 'php' },
            { scheme: 'file', language: 'xml' },
            { scheme: 'file', language: 'yml' },
            { scheme: 'file', language: 'yaml' },
            { scheme: 'file', language: 'twig' },
            { scheme: 'file', language: 'json' },
            { scheme: 'file', language: 'scss' }
          ],
          // Add output configuration
          outputChannel: this.outputChannel,
          traceOutputChannel: this.outputChannel,
          revealOutputChannelOn: RevealOutputChannelOn.Error,
          workspaceFolder: folder,
        };

        return new LanguageClient(
          'shopwareLSP',
          'Shopware Language Server',
          serverOptions,
          clientOptions
        );
    }

    private getServerPath(workspaceRoot: vscode.WorkspaceFolder): string | undefined {
        // Get the server path from settings or use default
        let serverPath = vscode.workspace.getConfiguration('shopwareLSP').get<string>('serverPath', '');
        
        // If no custom path is provided, use the bundled server
        if (!serverPath) {
            // For development, we'll look for the server in the parent directory
            const possiblePaths = [
            // When installed as extension
            this.context.asAbsolutePath(path.join('.', 'shopware-lsp')),
            // When installed as extension in the parent directory
            this.context.asAbsolutePath(path.join('..', 'shopware-lsp')),
            // When running from source
            path.join(workspaceRoot.uri.fsPath, '..', 'shopware-lsp'),
            // When in the same directory
            path.join(workspaceRoot.uri.fsPath, 'shopware-lsp')
            ];
    
            for (const p of possiblePaths) {
                if (fs.existsSync(p)) {
                    serverPath = p;
                    break;
                }
            }
        }
    
        if (!serverPath) {
            vscode.window.showErrorMessage('Could not find Symfony Service LSP server. Please set the path in settings.');
            return undefined;
        }

        return serverPath;
    }

    private getOuterMostWorkspaceFolder(folder: vscode.WorkspaceFolder): vscode.WorkspaceFolder {
        for (const element of this.sortedWorkspaceFolders) {
            let uri = folder.uri.toString();
            if (uri.charAt(uri.length - 1) !== '/') {
                uri = uri + '/';
            }
            if (uri.startsWith(element)) {
                return vscode.workspace.getWorkspaceFolder(vscode.Uri.parse(element))!;
            }
        }
        return folder;
    }

    private get sortedWorkspaceFolders(): string[] {
        if (this._sortedWorkspaceFolders === void 0) {
            this._sortedWorkspaceFolders = (vscode.workspace.workspaceFolders || [])
                .map((folder: vscode.WorkspaceFolder) => {
                    let result = folder.uri.toString();
                    if (result.charAt(result.length - 1) !== '/') {
                        result = result + '/';
                    }
                    return result;
                })
                .sort((a: string, b: string) =>  a.length - b.length)
        }

        return this._sortedWorkspaceFolders;
    }
}