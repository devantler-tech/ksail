/**
 * Status Bar Manager
 *
 * Manages the KSail status bar item showing workspace status.
 */

import * as vscode from "vscode";

/**
 * Status bar item manager
 */
export class StatusBarManager implements vscode.Disposable {
  private statusBarItem: vscode.StatusBarItem;

  constructor() {
    this.statusBarItem = vscode.window.createStatusBarItem(
      vscode.StatusBarAlignment.Left,
      100
    );
    this.statusBarItem.name = "KSail";
    this.statusBarItem.text = "$(ship) KSail";
    this.statusBarItem.tooltip = "KSail - Click to list clusters";
    this.statusBarItem.command = "ksail.cluster.list";

    // Check configuration
    const config = vscode.workspace.getConfiguration("ksail");
    if (config.get<boolean>("showStatusBar", true)) {
      this.statusBarItem.show();
    }
  }

  /**
   * Show the status bar item
   */
  show(): void {
    this.statusBarItem.show();
  }

  /**
   * Hide the status bar item
   */
  hide(): void {
    this.statusBarItem.hide();
  }

  /**
   * Dispose resources
   */
  dispose(): void {
    this.statusBarItem.dispose();
  }
}
