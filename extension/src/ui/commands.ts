// The workbench's commands, as the extension registers them: one table, so a test reaches the same handler the
// registered command runs (VS Code itself stays in extension.ts).
import type { Subject } from "../composition/slots.js";
import { CMD, type Workbench } from "./controller.js";

/** Each command id with its handler on the current workbench (undefined while it is being rebuilt). */
export function workbenchCommands(wb: () => Workbench | undefined): ReadonlyArray<readonly [string, (...args: never[]) => unknown]> {
  return [
    // the user's Refresh runs the poll job itself (skipped while a poll runs), never a second poll beside it
    [CMD.refresh, () => wb()?.refreshNow()],
    [CMD.openDecision, (msg: string) => wb()?.openDecision(msg)],
    [CMD.chooseOption, (msg: string, option: string) => wb()?.chooseOption(msg, option)],
    [CMD.openThread, (msg: string) => wb()?.openThread(msg)],
    [CMD.switchSurface, (name?: string) => wb()?.switchSurface(name)],
    [CMD.openSurfaceWindow, (name?: string) => wb()?.openSurfaceWindow(name)],
    [CMD.openRecorder, () => wb()?.openRecorder()],
    [CMD.ask, () => wb()?.askCommand()],
    [CMD.say, () => wb()?.sayCommand()],
    [CMD.compose, () => wb()?.composeCommand()],
    [CMD.openSlot, (key: string, subject: Subject) => wb()?.openSlot(key, subject)],
    [CMD.openEntry, (key: string) => wb()?.openEntry(key)],
  ];
}
