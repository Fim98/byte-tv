let retrySequence = 0;

export function retryCoverURL(address) {
  if (typeof address !== 'string' || !address.startsWith('/api/ui/image?')) return address;
  const hash = address.indexOf('#');
  const target = hash < 0 ? address : address.slice(0, hash);
  const fragment = hash < 0 ? '' : address.slice(hash);
  return target + (target.endsWith('?') ? '' : '&') + '_retry=' + Date.now().toString(36) + '-' + (++retrySequence).toString(36) + fragment;
}
