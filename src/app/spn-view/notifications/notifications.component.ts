import { ChangeDetectorRef, Component, OnInit } from "@angular/core";

import { CommonModule } from "@angular/common";
import { AlertButton, AlertController, IonicModule } from "@ionic/angular";
import { Action, Notification, NotificationType, getNotificationTypeString } from "src/app/services/notifications.types";
import { NotificationsService } from "src/app/services/notifications.service";

@Component({
  selector: "notifications",
  templateUrl: './notifications.component.html',
  styleUrls: ['./notifications.component.scss'],
  standalone: true,
  imports: [CommonModule, IonicModule]
})
export class NotificationComponent implements OnInit {
  readonly types = NotificationType;

  notifications: Notification[];

  constructor(
    private notificationService: NotificationsService,
    private alertController: AlertController,
    private changeDetector: ChangeDetectorRef) { }

  ngOnInit(): void {
    this.notificationService.new$
      .subscribe((notifications: Notification[]): void => {
        this.notifications = notifications;
        this.changeDetector.detectChanges();
      });
  }

  private escapeHtml(value: string | null | undefined): string {
    return String(value || '')
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#039;');
  }

  open(notification: Notification): void {
    if (!notification.AvailableActions) {
      return;
    }

    const buttons: AlertButton[] = [];
    notification.AvailableActions.forEach((action: Action): void => {
      buttons.push({
        text: action.Text,
        handler: (): void => {
          this.performAction(notification, action);
        }
      });
    });

    this.alertController.create({
      // Ionic alert messages can render markup. Treat all notification text as
      // data, even when it originates from Safing update/intel content.
      header: this.escapeHtml(notification.Title),
      subHeader: this.escapeHtml(getNotificationTypeString(notification.Type)),
      message: this.escapeHtml(notification.Message),
      buttons,
    }).then((alert) => {
      alert.present();
    });
  }

  performAction(notification: Notification, action: Action): void {
    this.notificationService.execute(notification, action).subscribe({
      error: err => console.error('Notification action failed', err)
    });
  }
}
