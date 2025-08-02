# MediaScreen Manager - Pairing Display Templates

This directory contains the HTML template for the pairing display feature.

## Template Files

- `pairing_display.html` - Main pairing display template with QR code support

## Configuration

To enable the pairing display, set `DeployPairingDisplay: true` in your client configuration:

```json
{
  "client_id": "your-client-id",
  "device_name": "Your Device Name",
  "deploy_pairing_display": true
}
```

## Template Variables

The template receives the following variables:

- `.Code` - The current pairing code (string)
- `.QRCodeImage` - Base64 encoded QR code PNG image (string)
- `.Expiry` - Formatted expiry time (string)

## Customization

You can customize the template by:

1. **Editing the existing template**: Modify `pairing_display.html` directly
2. **Using a custom path**: Set the `MSC_TEMPLATE_PATH` environment variable to point to a directory containing your custom `pairing_display.html`

### Example: Using Custom Template Path

```bash
export MSC_TEMPLATE_PATH=/path/to/your/templates
./msm-client start --deploy-pairing-display
```

## Template Path Resolution

The system looks for templates in this order:

1. `$MSC_TEMPLATE_PATH/pairing_display.html` (if environment variable is set)
2. `<executable-dir>/templates/pairing_display.html` (relative to executable)
3. `./templates/pairing_display.html` (relative to current working directory)

## Access URLs

When the pairing server is running with display enabled, you can access:

- `/` - Main pairing display
- `/qr` - QR code display
- `/pairing` - Alias for pairing display
- `/pair` - Generate new pairing code (API endpoint)
- `/pair/confirm` - Confirm pairing (API endpoint)

## Auto-refresh

The template includes JavaScript that automatically refreshes the page every 15 seconds to update the pairing code status and ensure fresh content.
