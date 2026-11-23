import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Form, Header, Message, Segment, Table } from 'semantic-ui-react';

import { API } from '../../helpers/api';

type RetainedChat = { id: number; user_id: number; purge_at: number };
type Access = { id: number; reviewer_id: number; session_id: number; reason: string; accessed_at: number };
type ChatMessage = { id: number; role: string; content: string; created_time: number };
type Page<T> = { rows: T[]; next: number };
type Result<T> = { success: boolean; data: Page<T> };

const endpoint = '/api/privacy/review';

export default function PrivacyReview() {
  const { t, i18n } = useTranslation();
  const [tab, setTab] = useState<'chats' | 'access'>('chats');
  const [before, setBefore] = useState(0);
  const [page, setPage] = useState<Page<RetainedChat | Access> | null>(null);
  const [selected, setSelected] = useState<RetainedChat | null>(null);
  const [review, setReview] = useState<Page<ChatMessage> | null>(null);
  const [reason, setReason] = useState('');
  const [reading, setReading] = useState(false);
  const [failed, setFailed] = useState(false);
  const [retry, setRetry] = useState(0);
  const format = (unix: number) => new Date(unix * 1000).toLocaleString(i18n.resolvedLanguage);

  useEffect(() => {
    const controller = new AbortController();
    API.get<Result<RetainedChat | Access>>(`${endpoint}/${tab}`, {
      params: { before }, signal: controller.signal,
    }).then(({ data }) => {
      if (!controller.signal.aborted) {
        if (data.success) setPage(data.data);
        else setFailed(true);
      }
    }).catch(() => { if (!controller.signal.aborted) setFailed(true); });
    return () => controller.abort();
  }, [before, retry, tab]);

  function resetPage() {
    setPage(null);
    setFailed(false);
    setReview(null);
    setSelected(null);
    setReason('');
  }

  async function openReview(after = 0) {
    if (!selected || !reason.trim()) return;
    setReading(true);
    setReview(null);
    setFailed(false);
    try {
      const { data } = await API.post<Result<ChatMessage>>(`${endpoint}/chats/${selected.id}`, {
        reason: reason.trim(), after,
      });
      if (data.success) setReview(data.data);
      else setFailed(true);
    } catch {
      setFailed(true);
    } finally {
      setReading(false);
    }
  }

  return (
    <Segment>
      <Header as='h1'>{t('privacyReview.title')}</Header>
      <p>{t('privacyReview.description')}</p>
      <Button.Group>
        <Button active={tab === 'chats'} disabled={reading} onClick={() => {
          if (tab === 'chats') return;
          resetPage(); setTab('chats'); setBefore(0);
        }}>{t('privacyReview.queue')}</Button>
        <Button active={tab === 'access'} disabled={reading} onClick={() => {
          if (tab === 'access') return;
          resetPage(); setTab('access'); setBefore(0);
        }}>{t('privacyReview.audit')}</Button>
      </Button.Group>

      {failed && <Message negative>
        <p>{t('privacyReview.failed')}</p>
        {!selected && <Button size='small' onClick={() => {
          setFailed(false); setRetry((count) => count + 1);
        }}>{t('privacyReview.retry')}</Button>}
      </Message>}
      {!page && !failed && <Message info role='status'>{t('privacyReview.loading')}</Message>}
      {page && (
        <>
          {page.rows.length ? (
            <Table>
              <Table.Header><Table.Row>
                <Table.HeaderCell>{t('privacyReview.session')}</Table.HeaderCell>
                <Table.HeaderCell>{t(tab === 'chats' ? 'privacyReview.user' : 'privacyReview.reviewer')}</Table.HeaderCell>
                <Table.HeaderCell>{t(tab === 'chats' ? 'privacyReview.until' : 'privacyReview.viewed')}</Table.HeaderCell>
                <Table.HeaderCell>{t(tab === 'chats' ? 'privacyReview.action' : 'privacyReview.reason')}</Table.HeaderCell>
              </Table.Row></Table.Header>
              <Table.Body>
                {page.rows.map((row) => (
                  'purge_at' in row ? (
                    <Table.Row key={row.id}>
                      <Table.Cell>{row.id}</Table.Cell>
                      <Table.Cell>{row.user_id}</Table.Cell>
                      <Table.Cell>{format(row.purge_at)}</Table.Cell>
                      <Table.Cell><Button size='small' disabled={reading} onClick={() => {
                        setSelected(row); setReason(''); setReview(null); setFailed(false);
                      }}>{t('privacyReview.action')}</Button></Table.Cell>
                    </Table.Row>
                  ) : (
                    <Table.Row key={row.id}>
                      <Table.Cell>{row.session_id}</Table.Cell>
                      <Table.Cell>{row.reviewer_id}</Table.Cell>
                      <Table.Cell>{format(row.accessed_at)}</Table.Cell>
                      <Table.Cell style={{ overflowWrap: 'anywhere' }}>{row.reason}</Table.Cell>
                    </Table.Row>
                  )
                ))}
              </Table.Body>
            </Table>
          ) : <Message>{t('privacyReview.empty')}</Message>}
          {page.next > 0 && <Button disabled={reading} onClick={() => {
            resetPage(); setBefore(page.next);
          }}>{t('privacyReview.next')}</Button>}
          {before > 0 && <Button disabled={reading} onClick={() => {
            resetPage(); setBefore(0);
          }}>{t('privacyReview.first')}</Button>}
        </>
      )}

      {selected && (
        <Segment>
          <Header as='h2'>{t('privacyReview.session')} #{selected.id}</Header>
          <Form onSubmit={() => void openReview()}>
            <Form.Input
              label={t('privacyReview.reason')} maxLength={300}
              value={reason} disabled={reading}
              onChange={(_, data) => { setReason(data.value); setReview(null); }}
            />
            <Button type='submit' primary disabled={reading || !reason.trim()} loading={reading}>
              {t('privacyReview.open')}
            </Button>
            <Button type='button' disabled={reading} onClick={() => {
              setSelected(null); setReview(null); setReason(''); setFailed(false);
            }}>{t('privacyReview.close')}</Button>
          </Form>
          {review && (
            <div aria-live='polite'>
              {review.rows.map((message) => (
                <Segment key={message.id}>
                  <strong>{t(message.role === 'user' ? 'privacyReview.user' : 'privacyReview.assistant')}</strong>
                  <p dir='auto' style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{message.content}</p>
                </Segment>
              ))}
              {!review.rows.length && <Message>{t('privacyReview.empty')}</Message>}
              {review.next > 0 && <Button onClick={() => void openReview(review.next)}>{t('privacyReview.next')}</Button>}
            </div>
          )}
        </Segment>
      )}
    </Segment>
  );
}
