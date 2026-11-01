import React, { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Plus, RefreshCw } from 'lucide-react';

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
import { statusBadgeVariant, workApi, WorkTaskView } from './workApi';

// Work task list (R3): the minimal entry over /api/studio/tasks — creation
// queues a run, the detail page follows progress and collects results.
const Work: React.FC = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [tasks, setTasks] = useState<WorkTaskView[]>([]);
  const [loading, setLoading] = useState(true);
  const [createOpen, setCreateOpen] = useState(false);
  const [title, setTitle] = useState('');
  const [goal, setGoal] = useState('');
  const [model, setModel] = useState('');
  const [models, setModels] = useState<string[]>([]);
  const [submitting, setSubmitting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await workApi.listTasks();
      setTasks(data || []);
    } catch {
      // toast already surfaced by the client helper
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

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
      const result = await workApi.createRun({ title: title.trim(), input: goal.trim(), model });
      setCreateOpen(false);
      setTitle('');
      setGoal('');
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
          <Button variant='ghost' size='icon' onClick={load} title={t('work.refresh')}>
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

      {loading ? (
        <p className='text-muted-foreground text-sm'>{t('work.loading')}</p>
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
                    ? new Date(task.updated_time * 1000).toLocaleString()
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
    </div>
  );
};


export default Work;
