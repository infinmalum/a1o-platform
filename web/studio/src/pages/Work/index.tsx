import { ListError, ListLoading, ListPages, useStudioPage } from '@/components/StudioList';
import React, { useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Bot, Plus, RefreshCw } from 'lucide-react';

import { formatStudioDate } from '@/helpers/render';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { agentsApi, type AgentView } from '@/pages/Agents/agentsApi';
import { chatApi, type ChatFile } from '@/pages/Chat/chatApi';
import { statusBadgeVariant, workApi } from './workApi';

// Work task list (R3): the minimal entry over /api/studio/tasks — creation
// queues a run, the detail page follows progress and collects results.
// R4: /work?agent=<id> deep links from the plaza and pins the agent version.
const Work: React.FC = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [createOpen, setCreateOpen] = useState(false);
  const [title, setTitle] = useState('');
  const [referenceImage, setReferenceImage] = useState<ChatFile>();
  const [goal, setGoal] = useState('');
  const [model, setModel] = useState('');
  const [models, setModels] = useState<string[]>([]);
  const [boundAgent, setBoundAgent] = useState<AgentView | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const taskList = useStudioPage(workApi.listTasks);
  const tasks = taskList.items;

  // Deep link from the agent plaza: prefill the create dialog with the
  // agent's frozen model and pin its latest version on the created run.
  const agentParam = searchParams.get('agent');
  useEffect(() => {
    if (!agentParam) return;
    agentsApi
      .getDetail(Number(agentParam))
      .then((agent) => {
        setBoundAgent(agent);
        if (agent.spec?.model) setModel(agent.spec.model);
        setCreateOpen(true);
        setSearchParams({}, { replace: true });
      })
      .catch(() => setSearchParams({}, { replace: true }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentParam]);

  const attachmentParam = Number(searchParams.get('attachment'));
  useEffect(() => {
    if (!attachmentParam) return;
    let cancelled = false;
    void chatApi.listFiles().then((files) => {
      if (cancelled) return;
      const image = files.find((file) => file.id === attachmentParam && file.kind === 'image');
      if (image) {
        setReferenceImage(image);
        setCreateOpen(true);
      }
      setSearchParams((previous) => {
        const next = new URLSearchParams(previous);
        next.delete('attachment');
        return next;
      }, { replace: true });
    }).catch(() => undefined);
    return () => { cancelled = true; };
  }, [attachmentParam, setSearchParams]);

  useEffect(() => {
    if (!createOpen) return;
    workApi
      .listModels()
      .then((data) => {
        const list = data || [];
        setModels(list);
        setModel((current) => current || list[0] || '');
      })
      .catch(() => undefined);
  }, [createOpen]);

  const submit = async () => {
    if (!goal.trim() || !model || submitting) return;
    setSubmitting(true);
    try {
      const result = await workApi.createRun({
        title: title.trim(),
        input: goal.trim(),
        model,
        agent_version_id: boundAgent?.latest.id,
        reference_file_id: referenceImage?.id,
      });
      setCreateOpen(false);
      setTitle('');
      setGoal('');
      setReferenceImage(undefined);
      setBoundAgent(null);
      navigate(`/work/${result.task?.id ?? result.run.task_id}`);
    } catch {
      // toast already surfaced
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className='mx-auto w-full max-w-6xl px-4 py-10'>
      <div className='mb-8 flex items-center justify-between'>
        <div>
          <h1 className='text-2xl font-semibold tracking-tight'>{t('work.title')}</h1>
          <p className='text-muted-foreground mt-1 text-sm'>{t('work.tagline')}</p>
        </div>
        <div className='flex items-center gap-2'>
          <Button variant='ghost' size='icon' onClick={taskList.reload} title={t('work.refresh')}>
            <RefreshCw />
          </Button>
          <Dialog open={createOpen} onOpenChange={setCreateOpen}>
            <DialogTrigger asChild>
              <Button size='sm'>
                <Plus /> {t('work.create')}
              </Button>
            </DialogTrigger>
            <DialogContent className='sm:max-w-lg'>
              <DialogHeader>
                <DialogTitle>{t('work.create')}</DialogTitle>
                <DialogDescription>{t('work.createHint')}</DialogDescription>
              </DialogHeader>
              <div className='grid gap-4 py-2'>
                {boundAgent && (
                  <div className='flex items-center gap-2 rounded-md border px-3 py-2 text-sm'>
                    <Bot className='size-4' />
                    <span className='font-medium'>{boundAgent.name}</span>
                    <Badge variant='secondary'>v{boundAgent.current_version}</Badge>
                  </div>
                )}
                <div className='grid gap-2'>
                  <Label htmlFor='work-title'>{t('work.titleLabel')}</Label>
                  <Input
                    id='work-title'
                    value={title}
                    onChange={(event) => setTitle(event.target.value)}
                    placeholder={t('work.titlePlaceholder')}
                  />
                </div>
                <div className='grid gap-2'>
                  {referenceImage && (
                    <div className='flex flex-wrap items-center gap-2'>
                      <Badge variant='secondary'>{referenceImage.name}</Badge>
                      <Button size='sm' variant='ghost' disabled={submitting} onClick={() => setReferenceImage(undefined)}>
                        {t('images.removeReference')}
                      </Button>
                    </div>
                  )}
                  <Label htmlFor='work-goal'>{t('work.goalLabel')}</Label>
                  <Textarea
                    id='work-goal'
                    value={goal}
                    onChange={(event) => setGoal(event.target.value)}
                    placeholder={t('work.goalPlaceholder')}
                    rows={5}
                  />
                </div>
                <div className='grid gap-2'>
                  <Label>{t('work.modelLabel')}</Label>
                  <Select value={model} onValueChange={setModel}>
                    <SelectTrigger className='w-full'>
                      <SelectValue placeholder={t('work.modelPlaceholder')} />
                    </SelectTrigger>
                    <SelectContent>
                      {models.map((name) => (
                        <SelectItem key={name} value={name}>
                          {name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>
              <DialogFooter>
                <Button onClick={submit} disabled={!goal.trim() || !model || submitting}>
                  {t('work.submit')}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </div>
      </div>

      {taskList.loading ? (
        <ListLoading cards />
      ) : taskList.failed ? (
        <ListError retry={taskList.reload} />
      ) : tasks.length === 0 ? (
        <p className='text-muted-foreground text-sm'>{t('work.empty')}</p>
      ) : (
        <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
          {tasks.map((task) => (
            <Card
              key={task.id}
              className='cursor-pointer transition-colors hover:border-foreground/30'
              onClick={() => navigate(`/work/${task.id}`)}
            >
              <CardHeader>
                <CardTitle className='line-clamp-2 text-base'>{task.title}</CardTitle>
                <CardDescription>
                  {task.updated_time
                    ? formatStudioDate(task.updated_time)
                    : ''}
                </CardDescription>
                <CardAction>
                  {task.latest_run ? (
                    <Badge variant={statusBadgeVariant(task.latest_run.status)}>
                      {t(`work.status.${task.latest_run.status}`)}
                    </Badge>
                  ) : null}
                </CardAction>
              </CardHeader>
              {task.summary ? (
                <CardContent>
                  <p className='text-muted-foreground line-clamp-3 text-sm'>{task.summary}</p>
                </CardContent>
              ) : null}
            </Card>
          ))}
        </div>
      )}
      {!taskList.loading && !taskList.failed && <ListPages page={taskList.page} more={taskList.more} onPage={taskList.setPage} />}
    </div>
  );
};


export default Work;
