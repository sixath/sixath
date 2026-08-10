async (page) => {
  const before = await page.getByRole('button', { name: 'Rewind here' }).count();
  const beforeMsgs = await page.locator('[class*="chat-msg"]').count();
  const userText = await page.getByText('Reply with exactly one word: browserok').count();

  // Accept confirm dialog then click first Rewind.
  page.once('dialog', async (dialog) => {
    await dialog.accept();
  });
  await page.getByRole('button', { name: 'Rewind here' }).first().click();

  // Wait for rewind UI settle: either no rewind buttons or message count drop.
  await page.waitForTimeout(1500);
  const after = await page.getByRole('button', { name: 'Rewind here' }).count();
  const afterMsgs = await page.locator('[class*="chat-msg"]').count();
  const userTextAfter = await page.getByText('Reply with exactly one word: browserok').count();
  const url = page.url();

  return {
    beforeRewindButtons: before,
    afterRewindButtons: after,
    beforeMsgs,
    afterMsgs,
    userTextBefore: userText,
    userTextAfter,
    url,
    ok: afterMsgs < beforeMsgs || userTextAfter < userTextBefore || after < before,
  };
}
