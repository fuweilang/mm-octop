/** Matches the desktop shell cap in desktop/src/filedrop.go. */
export const MAX_CHAT_UPLOAD_FILES = 500;

export interface DropSnapshot {
  entries: FileSystemEntry[];
  files: File[];
  /** True when the drop contained at least one file or folder item. */
  hadFileItems: boolean;
}

export interface CollectedDrop {
  files: File[];
  truncated: boolean;
}

function readEntryFile(entry: FileSystemFileEntry): Promise<File> {
  return new Promise((resolve, reject) => {
    entry.file(resolve, reject);
  });
}

/**
 * Relative paths seen during a directory walk, keyed by the ``File`` object.
 *
 * ``File`` deliberately has no writable path field, and dropped files lose
 * ``webkitRelativePath``; a WeakMap lets the upload keep the folder tree
 * without changing the ``File[]`` shape every caller and test already expects.
 */
const droppedRelativePaths = new WeakMap<File, string>();

export function rememberRelativePath(file: File, relativePath?: string): void {
  const clean = (relativePath ?? "").replace(/^\/+/, "");
  if (clean) droppedRelativePaths.set(file, clean);
}

/** Relative path for an upload: drop-walk result first, then ``webkitRelativePath``. */
export function relativePathOf(file: File): string | undefined {
  const remembered = droppedRelativePaths.get(file);
  if (remembered) return remembered;
  const native = (file as File & { webkitRelativePath?: string })
    .webkitRelativePath;
  const clean = (native ?? "").replace(/^\/+/, "");
  return clean || undefined;
}

function readAllDirectoryEntries(
  reader: FileSystemDirectoryReader,
): Promise<FileSystemEntry[]> {
  return new Promise((resolve, reject) => {
    const all: FileSystemEntry[] = [];
    const readBatch = () => {
      reader.readEntries((batch) => {
        if (batch.length === 0) {
          resolve(all);
          return;
        }
        all.push(...batch);
        readBatch();
      }, reject);
    };
    readBatch();
  });
}

/** Returns true when another entry exists past the cap. */
async function collectEntryFiles(
  entry: FileSystemEntry,
  out: File[],
  max: number,
): Promise<boolean> {
  if (out.length >= max) return true;
  if (entry.isFile) {
    const file = await readEntryFile(entry as FileSystemFileEntry);
    rememberRelativePath(file, entry.fullPath || file.name);
    out.push(file);
    return false;
  }
  if (!entry.isDirectory) return false;
  const reader = (entry as FileSystemDirectoryEntry).createReader();
  const children = await readAllDirectoryEntries(reader);
  for (const child of children) {
    if (await collectEntryFiles(child, out, max)) return true;
  }
  return false;
}

export function dragHasFiles(dataTransfer: DataTransfer | null): boolean {
  if (!dataTransfer?.types) return false;
  return Array.from(dataTransfer.types).includes("Files");
}

/**
 * Capture drop payload synchronously. Directory entries stay readable after
 * the drop event ends; ``File`` objects from ``getAsFile`` must be taken now.
 */
export function snapshotDroppedEntries(
  dataTransfer: DataTransfer | null,
): DropSnapshot {
  if (!dataTransfer) {
    return { entries: [], files: [], hadFileItems: false };
  }

  const entries: FileSystemEntry[] = [];
  const loose: File[] = [];
  const items = dataTransfer.items;
  let sawFileItem = false;

  if (items && items.length > 0) {
    for (let i = 0; i < items.length; i += 1) {
      const item = items[i];
      if (!item || item.kind !== "file") continue;
      sawFileItem = true;
      const entry =
        typeof item.webkitGetAsEntry === "function"
          ? item.webkitGetAsEntry()
          : null;
      if (entry) {
        entries.push(entry);
        continue;
      }
      const file = item.getAsFile();
      if (file) loose.push(file);
    }
  }

  if (sawFileItem) {
    return { entries, files: loose, hadFileItems: true };
  }

  const fallback = Array.from(dataTransfer.files ?? []);
  return {
    entries: [],
    files: fallback,
    hadFileItems: fallback.length > 0,
  };
}

export async function filesFromSnapshot(
  snapshot: DropSnapshot,
): Promise<CollectedDrop> {
  const out: File[] = [];
  let truncated = false;
  for (const entry of snapshot.entries) {
    if (await collectEntryFiles(entry, out, MAX_CHAT_UPLOAD_FILES)) {
      truncated = true;
      break;
    }
  }
  if (!truncated) {
    const room = MAX_CHAT_UPLOAD_FILES - out.length;
    if (snapshot.files.length > room) {
      out.push(...snapshot.files.slice(0, room));
      truncated = true;
    } else {
      out.push(...snapshot.files);
    }
  }
  return { files: out, truncated };
}
