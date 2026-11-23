export function uploadSize(size: number) {
  if (size >= 1024 * 1024) return `${+(size / (1024 * 1024)).toPrecision(4)} MiB`;
  if (size >= 1024) return `${+(size / 1024).toPrecision(4)} KiB`;
  return `${size} B`;
}

// Recognize the same image signatures as the server's HTTP content sniffer
// for files above the text limit, even when the browser omitted the MIME type.
export async function isChatImage(file: File) {
  const header = new Uint8Array(await file.slice(0, 14).arrayBuffer());
  const matches = (...prefix: number[]) => prefix.every((value, index) => header[index] === value);
  const ascii = String.fromCharCode(...header);
  return matches(0, 0, 1, 0) || matches(0, 0, 2, 0) ||
    ascii.startsWith('BM') || ascii.startsWith('GIF87a') || ascii.startsWith('GIF89a') ||
    (ascii.startsWith('RIFF') && ascii.slice(8, 14) === 'WEBPVP') ||
    matches(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a) ||
    matches(0xff, 0xd8, 0xff);
}
