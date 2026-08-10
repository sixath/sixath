async (page) => {
  const beforeBtns = await page.getByRole('button', { name: 'Rewind here' }).count();
  const beforeUser = await page.getByText('Reply with exactly one word: browserok').count();
  const beforeBubble = await page.locator('.chat-msg').count();

  const dialogPromise = page.waitForEvent('dialog', { timeout: 5000 });
  const clickPromise = page.getByRole('button', { name: 'Rewind here' }).first().click();
  const dialog = await dialogPromise;
  const msg = dialog.message();
  await dialog.accept();
  await clickPromise;
  await page.waitForTimeout(2000);

  const afterBtns = await page.getByRole('button', { name: 'Rewind here' }).count();
  const afterUser = await page.getByText('Reply with exactly one word: browserok').count();
  const afterBubble = await page.locator('.chat-msg').count();
  const body = (await page.locator('main').innerText()).slice(0, 500);

  return {
    dialogMsg: msg,
    beforeBtns,
    afterBtns,
    beforeUser,
    afterUser,
    beforeBubble,
    afterBubble,
    shortened: afterBubble < beforeBubble || afterUser < beforeUser || afterBtns < beforeBtns,
    bodySnippet: body,
    url: page.url(),
  };
}
