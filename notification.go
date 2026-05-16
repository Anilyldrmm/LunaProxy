//go:build windows

package main

import "fmt"

// showToast — Windows 10+ toast bildirimi. PowerShell WinRT API ile gönderilir.
// Async çalışır; hata sessizce yutulur (bildirim kritik değil).
func showToast(title, message string) {
	script := fmt.Sprintf(`
[void][Windows.UI.Notifications.ToastNotificationManager,Windows.UI.Notifications,ContentType=WindowsRuntime]
[void][Windows.Data.Xml.Dom.XmlDocument,Windows.Data.Xml.Dom.XmlDocument,ContentType=WindowsRuntime]
try {
  $xml = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent(
    [Windows.UI.Notifications.ToastTemplateType]::ToastText02)
  $nodes = $xml.GetElementsByTagName('text')
  $nodes.Item(0).AppendChild($xml.CreateTextNode('%s')) | Out-Null
  $nodes.Item(1).AppendChild($xml.CreateTextNode('%s')) | Out-Null
  $toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
  [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('LunaProxy').Show($toast)
} catch {}
`, title, message)
	go hiddenRun("powershell", "-Command", script)
}
