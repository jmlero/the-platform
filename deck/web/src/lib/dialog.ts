/** Native modality keeps the background inert; restore the opener before teardown. */
export function modal(node: HTMLDialogElement) {
  const opener = document.activeElement;
  node.showModal();
  // Svelte may mount the dialog's contents after its action runs.
  queueMicrotask(() => {
    if (node.isConnected && node.open) {
      node.querySelector<HTMLElement>('[data-dialog-focus]')?.focus();
    }
  });
  return {
    destroy() {
      node.close();
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
    }
  };
}
