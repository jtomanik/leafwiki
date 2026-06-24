import { Input } from '@/components/ui/input'

export type FormInputError = {
  message: string
  code?: string
  messageId?: string
}

type FormInputProps = {
  label?: string
  name?: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  testid?: string
  error?: string | FormInputError
  errorCode?: string
  errorMessageId?: string
  errorTestId?: string
  type?: string
  autoComplete?: string
  autoFocus?: boolean
  readOnly?: boolean
  allowedHotkeys?: string
}

export function FormInput({
  label,
  name,
  value,
  autoComplete,
  autoFocus,
  onChange,
  testid,
  placeholder,
  error,
  errorCode,
  errorMessageId,
  errorTestId,
  type = 'text',
  readOnly = false,
  allowedHotkeys,
}: FormInputProps) {
  const errorMessage = typeof error === 'string' ? error : error?.message
  const resolvedErrorCode =
    errorCode ?? (typeof error === 'string' ? undefined : error?.code)
  const resolvedErrorMessageId =
    errorMessageId ?? (typeof error === 'string' ? undefined : error?.messageId)

  return (
    <div className="form-input">
      {label && <label className="form-input__label">{label}</label>}
      <Input
        autoFocus={autoFocus || false}
        name={name}
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        autoComplete={autoComplete}
        readOnly={readOnly}
        className={errorMessage ? 'form-input__input-error' : ''}
        data-testid={testid}
        data-allow-hotkeys={allowedHotkeys}
      />
      {errorMessage && (
        <p
          className="form-input__error"
          data-testid={errorTestId}
          data-error-code={resolvedErrorCode}
          data-l10n-id={resolvedErrorMessageId}
        >
          {errorMessage}
        </p>
      )}
    </div>
  )
}
