for (const button of document.querySelectorAll('[data-copy]')) {
  if (!navigator.clipboard) break;

  const box = button.closest('.code-box');
  const status = box.querySelector('.copy-status');
  button.hidden = false;
  button.addEventListener('click', async () => {
    status.textContent = '';
    try {
      await navigator.clipboard.writeText(box.querySelector('code').textContent);
      button.textContent = 'Copied';
      status.textContent = 'Commands copied to the clipboard.';
    } catch {
      button.textContent = 'Copy failed';
      status.textContent = 'Select the commands to copy them manually.';
    }
  });
}
