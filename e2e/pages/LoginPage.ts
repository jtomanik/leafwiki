import { expect, Page } from '@playwright/test';
import { toAppPath } from './appPath';

export default class LoginPage {
  constructor(private page: Page) {}

  async goto() {
    await this.page.goto(toAppPath('/login'));
    await this.page.locator('input[data-testid="login-identifier"]').waitFor({ state: 'visible' });
  }

  async getUsernameInput() {
    return this.page.locator('input[data-testid="login-identifier"]');
  }

  async getPasswordInput() {
    return this.page.locator('input[data-testid="login-password"]');
  }

  async getSubmitButton() {
    return this.page.locator('button[data-testid="login-submit"]');
  }

  async expectInvalidCredentialsError() {
    const message = this.page.getByTestId('login-error-message');
    await expect(message).toHaveAttribute('data-error-code', 'auth_invalid_credentials');
    await expect(message).toHaveAttribute('data-l10n-id', 'errors.auth.invalid_credentials');
  }

  async login(identifier: string, password: string) {
    const identifierInput = await this.getUsernameInput();
    const passwordInput = await this.getPasswordInput();
    const submitBtn = await this.getSubmitButton();

    await identifierInput.fill(identifier);
    await passwordInput.fill(password);
    await submitBtn.click();
  }
}
