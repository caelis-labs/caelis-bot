// Magpie trusts requests from this computer. A custom port does not turn a
// loopback gateway into a credential form; remote addresses still need auth.
export function localMagpie(provider: string | undefined, address: string): boolean {
 if (provider !== 'magpie') return false;
 try {
  const url = new URL(address);
  return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password &&
   ['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname);
 } catch { return false; }
}
