import React from 'react';
import { Card, Icon, Modal } from 'semantic-ui-react';
import type { IconProps } from 'semantic-ui-react';
import { useTranslation } from 'react-i18next';

import { UiMode } from '../helpers/uiMode';

interface ModeChooserProps {
  open: boolean;
  onChoose: (mode: UiMode) => void;
}

// ModeChooser asks a new account what it is here for. It is deliberately a
// one-question dialog: everything it decides (which pages the navigation shows,
// where sign-in lands) can be changed later from the settings page.
const ModeChooser = ({ open, onChoose }: ModeChooserProps) => {
  const { t } = useTranslation();
  const options: {
    mode: UiMode;
    icon: IconProps['name'];
    title: string;
    description: string;
  }[] = [
    {
      mode: 'chat',
      icon: 'comments',
      title: t('uiMode.chat.title'),
      description: t('uiMode.chat.description'),
    },
    {
      mode: 'api',
      icon: 'plug',
      title: t('uiMode.api.title'),
      description: t('uiMode.api.description'),
    },
    {
      mode: 'both',
      icon: 'grid layout',
      title: t('uiMode.both.title'),
      description: t('uiMode.both.description'),
    },
  ];

  return (
    <Modal
      open={open}
      size='small'
      closeOnDimmerClick={false}
      closeOnEscape={false}
    >
      <Modal.Header>{t('uiMode.title')}</Modal.Header>
      <Modal.Content>
        <p style={{ color: '#666', marginBottom: '1.5em' }}>
          {t('uiMode.subtitle')}
        </p>
        <Card.Group itemsPerRow={3} stackable>
          {options.map((option) => (
            <Card
              key={option.mode}
              link
              onClick={() => onChoose(option.mode)}
              style={{
                boxShadow: '0 4px 16px rgba(33, 136, 208, 0.12)',
                borderRadius: 12,
              }}
            >
              <Card.Content textAlign='center' style={{ padding: '1.5em 1em' }}>
                <Icon name={option.icon} size='big' color='blue' />
                <Card.Header style={{ marginTop: '0.5em' }}>
                  {option.title}
                </Card.Header>
                <Card.Description style={{ color: '#666', marginTop: '0.5em' }}>
                  {option.description}
                </Card.Description>
              </Card.Content>
            </Card>
          ))}
        </Card.Group>
        <p style={{ color: '#999', marginTop: '1.5em' }}>
          {t('uiMode.hint')}
        </p>
      </Modal.Content>
    </Modal>
  );
};

export default ModeChooser;
